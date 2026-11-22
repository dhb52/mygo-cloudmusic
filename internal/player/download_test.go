package player

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/waygu-music-plugin/sdk/pluginapi"
)

// downloadStream is a stream served by a test server.
func downloadStream(server *httptest.Server) *pluginapi.Stream {
	return &pluginapi.Stream{URL: server.URL, Type: "mp3", Level: "standard"}
}

// fastRetries takes the waits out of a retried download, leaving how many
// tries it makes.
func fastRetries(t *testing.T) {
	t.Helper()
	previous := downloadAttempts
	downloadAttempts = make([]time.Duration, len(downloadAttempts))
	t.Cleanup(func() { downloadAttempts = previous })
}

// leftOver lists the files in dir that a download should not have left.
func leftOver(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A whole song lands at its path, with the progress reaching its size and
// nothing left beside it.
func TestDownloadFileKeepsTheWholeSong(t *testing.T) {
	body := bytes.Repeat([]byte("abcdefgh"), 32<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	defer server.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "歌手 - 歌名.mp3")

	var done, total int64
	n, err := DownloadFile(context.Background(), downloadStream(server), dest, func(d, all int64) { done, total = d, all })
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if n != int64(len(body)) || done != n || total != n {
		t.Fatalf("read %d, progress %d of %d, want %d", n, done, total, len(body))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("the file holds %d bytes, want %d", len(got), len(body))
	}
	if names := leftOver(t, dir); len(names) != 1 || names[0] != "歌手 - 歌名.mp3" {
		t.Fatalf("the directory holds %v", names)
	}
}

// A body that stops short of the length it announced is fetched again, and
// fails once the tries are gone, naming both lengths and leaving no file.
func TestDownloadFileRefusesShortBody(t *testing.T) {
	fastRetries(t)
	var attempts int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&attempts, 1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 4096\r\nContent-Type: audio/mpeg\r\n\r\n")
		conn.Write(bytes.Repeat([]byte("x"), 100))
	}))
	defer server.Close()
	dir := t.TempDir()

	n, err := DownloadFile(context.Background(), downloadStream(server), filepath.Join(dir, "song.mp3"), nil)
	if !errors.Is(err, ErrDownloadShort) {
		t.Fatalf("DownloadFile gave %v, want a short download", err)
	}
	if n != 100 {
		t.Fatalf("read %d bytes, want the 100 that came", n)
	}
	if got := atomic.LoadInt64(&attempts); got != int64(len(downloadAttempts)) {
		t.Fatalf("the song was fetched %d times, want %d", got, len(downloadAttempts))
	}
	for _, want := range []string{"4096", "100"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not say %s", err, want)
		}
	}
	if names := leftOver(t, dir); len(names) != 0 {
		t.Fatalf("a failed download left %v", names)
	}
}

// A stream that comes in with nothing in it is not a download.
func TestDownloadFileRefusesEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "0")
	}))
	defer server.Close()
	dir := t.TempDir()

	if _, err := DownloadFile(context.Background(), downloadStream(server), filepath.Join(dir, "song.mp3"), nil); !errors.Is(err, ErrDownloadShort) {
		t.Fatalf("DownloadFile gave %v, want a short download", err)
	}
	if names := leftOver(t, dir); len(names) != 0 {
		t.Fatalf("an empty download left %v", names)
	}
}

// A body of no announced length comes in whole, as EOF says it is over.
func TestDownloadFileComesInWithoutALength(t *testing.T) {
	body := bytes.Repeat([]byte("y"), 96<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for len(body) > 0 {
			n := min(8<<10, len(body))
			w.Write(body[:n])
			w.(http.Flusher).Flush()
			body = body[n:]
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "song.mp3")

	n, err := DownloadFile(context.Background(), downloadStream(server), dest, nil)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if info, err := os.Stat(dest); err != nil || info.Size() != n {
		t.Fatalf("the file is %v at %d bytes, want %d", err, n, n)
	}
}

// A download the listener stops leaves no part file behind.
func TestDownloadFileCancelLeavesNothing(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(1<<20))
		w.Write(bytes.Repeat([]byte("z"), 8<<10))
		w.(http.Flusher).Flush()
		close(started)
		<-release
	}))
	defer server.Close()
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := DownloadFile(ctx, downloadStream(server), filepath.Join(dir, "song.mp3"), func(int64, int64) {
			select {
			case <-started:
				cancel()
			default:
			}
		})
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DownloadFile gave %v, want a canceled download", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled download did not end")
	}
	close(release)
	if names := leftOver(t, dir); len(names) != 0 {
		t.Fatalf("a canceled download left %v", names)
	}
}

// A song's name comes out as the file system wants it: no path separators,
// no control characters, and short enough to hold.
func TestReserveTargetCleansTheName(t *testing.T) {
	dir := t.TempDir()
	track := &pluginapi.Track{
		Name:    strings.Repeat("长", 80) + "/前半:后半*\x01 ",
		Artists: []pluginapi.Artist{{Name: "歌手/一"}, {Name: "歌手二"}},
	}
	target, err := ReserveTarget(dir, track, "flac", 0)
	if err != nil {
		t.Fatal(err)
	}
	name := target.Base
	for _, bad := range []string{"/", "\\", ":", "*", "\x01"} {
		if strings.Contains(name, bad) {
			t.Fatalf("the name %q still holds %q", name, bad)
		}
	}
	if len(name) > maxNameBytes {
		t.Fatalf("the name %q is %d bytes", name, len(name))
	}
	if !strings.HasPrefix(name, "歌手_一, 歌手二 - ") {
		t.Fatalf("the name %q does not lead with the artists", name)
	}
	if filepath.Ext(target.Path) != ".flac" {
		t.Fatalf("the path %q does not end in the format", target.Path)
	}

	// What a file system treats as an empty name, or as a hidden file,
	// is trimmed from either end.
	odd := &pluginapi.Track{Name: " .歌名. ", Artists: []pluginapi.Artist{{Name: "歌手"}}}
	if target, err := ReserveTarget(t.TempDir(), odd, "mp3", 0); err != nil || target.Base != "歌手 - .歌名" {
		t.Fatalf("the name %q came out as %q (%v)", odd.Name, target.Base, err)
	}
	if target, err := ReserveTarget(t.TempDir(), &pluginapi.Track{Name: " . "}, "mp3", 0); err != nil || target.Base != "未知歌曲" {
		t.Fatalf("a name of dots and spaces came out as %q (%v)", target.Base, err)
	}
}

// A song already in the directory, of the size announced, is the download;
// another file of its name pushes the download to a copy of its own.
func TestReserveTargetFindsTheSongOrMakesRoom(t *testing.T) {
	dir := t.TempDir()
	track := &pluginapi.Track{Name: "歌名", Artists: []pluginapi.Artist{{Name: "歌手"}}}
	first := filepath.Join(dir, "歌手 - 歌名.mp3")

	// A part file from an interrupted download does not hold the name.
	if err := os.WriteFile(first+".part", []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, err := ReserveTarget(dir, track, "mp3", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != first || target.Exists {
		t.Fatalf("ReserveTarget gave %+v, want the song's own name", target)
	}

	if err := os.WriteFile(first, bytes.Repeat([]byte("a"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if target, err = ReserveTarget(dir, track, "mp3", 4096); err != nil || !target.Exists || target.Path != first {
		t.Fatalf("ReserveTarget gave %+v (%v), want the file that is there", target, err)
	}
	if err := os.WriteFile(first, bytes.Repeat([]byte("a"), 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if target, err = ReserveTarget(dir, track, "mp3", 4096); err != nil || target.Exists || target.Path != filepath.Join(dir, "歌手 - 歌名 (2).mp3") {
		t.Fatalf("ReserveTarget gave %+v (%v), want a copy", target, err)
	}
}

// A translated lyric goes under the line of its own time, as one file.
func TestWriteLyricsPutsTheTranslationUnderIt(t *testing.T) {
	dir := t.TempDir()
	lyric := "[ti:歌名]\n[00:01.00]第一句\n[00:05.50]第二句\n"
	translation := "[00:01.00]first line\n[00:05.50]second line\n"

	path, err := WriteLyrics(dir, "歌手 - 歌名", lyric, translation)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[ti:歌名]\n[00:01.00]第一句\nfirst line\n[00:05.50]第二句\nsecond line\n"
	if string(body) != want {
		t.Fatalf("the lyric is\n%q\nwant\n%q", body, want)
	}
}

// A lyric on its own is written as it came, and an empty one is not written.
func TestWriteLyricsKeepsAnUntranslatedSong(t *testing.T) {
	dir := t.TempDir()
	lyric := "[00:01.00]只有一句\n"

	path, err := WriteLyrics(dir, "song", lyric, "")
	if err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != lyric {
		t.Fatalf("the lyric is %q (%v), want it as it came", body, err)
	}
	if path, err = WriteLyrics(dir, "empty", "", ""); err != nil || path != "" {
		t.Fatalf("an empty lyric gave %q (%v), want no file", path, err)
	}
}

// The cover takes the extension its own bytes ask for, and what is already
// there stays as the listener has it.
func TestWriteCoverFollowsTheBytes(t *testing.T) {
	dir := t.TempDir()
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte("j"), 64)...)

	path, err := WriteCover(dir, "歌手 - 歌名", jpeg)
	if err != nil || path != filepath.Join(dir, "歌手 - 歌名.jpg") {
		t.Fatalf("WriteCover gave %q (%v), want a jpeg", path, err)
	}
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCover(dir, "歌手 - 歌名", jpeg); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(path); string(body) != "mine" {
		t.Fatalf("the cover is %q, want what was there", body)
	}
	if path, err = WriteCover(dir, "别的", []byte("<html></html>")); err != nil || path != "" {
		t.Fatalf("WriteCover gave %q (%v), want nothing for a page", path, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "别的.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a page was written as a cover: %v", err)
	}
}

// A destination name that ran out of copies is reported rather than
// overwriting one of them.
func TestReserveTargetRefusesTooManyCopies(t *testing.T) {
	dir := t.TempDir()
	track := &pluginapi.Track{Name: "歌名", Artists: []pluginapi.Artist{{Name: "歌手"}}}
	for try := 1; try <= maxNameTries; try++ {
		name := "歌手 - 歌名"
		if try > 1 {
			name = fmt.Sprintf("%s (%d)", name, try)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".mp3"), []byte(strconv.Itoa(try)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ReserveTarget(dir, track, "mp3", 0); err == nil {
		t.Fatal("ReserveTarget took a name that was all used up")
	}
}

// A song the cache holds already is found for a download to copy, and a
// cache that is gone is no song at all.
func TestCachedPathFindsTheSongOfTheLevel(t *testing.T) {
	dir := t.TempDir()
	cache := NewCache(dir, 1<<20)
	owner := pluginapi.ID("qqmusic")
	track := &pluginapi.Track{ID: 7, Name: "歌名", Artists: []pluginapi.Artist{{Name: "歌手"}}}
	if _, _, ok := cache.CachedPath(owner, track, "exhigh"); ok {
		t.Fatal("an empty cache holds the song")
	}

	key := cacheKey(trackCacheRef(owner, track), "exhigh")
	body := []byte("audio")
	if err := os.WriteFile(filepath.Join(dir, key+".mp3"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(pluginapi.Stream{Type: "mp3", Level: "exhigh", Size: int64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}

	path, stream, ok := cache.CachedPath(owner, track, "exhigh")
	if !ok || path != filepath.Join(dir, key+".mp3") || stream == nil || stream.Level != "exhigh" {
		t.Fatalf("CachedPath gave %q, %+v, %v", path, stream, ok)
	}
	if _, _, ok := (*Cache)(nil).CachedPath(owner, track, "exhigh"); ok {
		t.Fatal("a nil cache holds the song")
	}

	// A song kept at a higher level is not the song of this one: a
	// download would hold lossless bytes under a "standard" name.
	if err := os.Remove(filepath.Join(dir, key+".mp3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, key+".json")); err != nil {
		t.Fatal(err)
	}
	lossless := cacheKey(trackCacheRef(owner, track), "lossless")
	if err := os.WriteFile(filepath.Join(dir, lossless+".flac"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err = json.Marshal(pluginapi.Stream{Type: "flac", Level: "lossless", Size: int64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, lossless+".json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}
	if path, _, ok := cache.CachedPath(owner, track, "standard"); ok {
		t.Fatalf("the standard level found the lossless song at %q", path)
	}
	if _, stream, ok := cache.CachedPath(owner, track, "lossless"); !ok || stream.Level != "lossless" {
		t.Fatal("the lossless level did not find its own song")
	}
}

// A copy of a song the cache holds lands whole, with nothing left over,
// and one of a file that is gone reports it.
func TestCopyFileLandsWhole(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cached.mp3")
	body := bytes.Repeat([]byte("c"), 40<<10)
	if err := os.WriteFile(src, body, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out", "歌手 - 歌名.mp3")

	n, err := CopyFile(src, dest)
	if err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if n != int64(len(body)) {
		t.Fatalf("copied %d bytes, want %d", n, len(body))
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("the copy is %v bytes (%v)", len(got), err)
	}
	if names := leftOver(t, filepath.Dir(dest)); len(names) != 1 {
		t.Fatalf("the copy left %v", names)
	}
	if _, err := CopyFile(filepath.Join(dir, "missing.mp3"), filepath.Join(dir, "out", "x.mp3")); err == nil {
		t.Fatal("CopyFile copied a file that is not there")
	}
}
