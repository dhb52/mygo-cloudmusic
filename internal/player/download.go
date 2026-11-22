package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/network"
)

// A download writes a song the listener keeps: the audio into the directory
// they chose, with its lyric and its cover beside it, under a name taken
// from the song. Unlike the cache it is never dropped for taking room, and
// one that comes in short fails rather than being kept.

// ErrDownloadShort is the body not being what it said it was, so that the
// file the listener would have kept is not written at all.
var ErrDownloadShort = errors.New("下载的文件不完整")

// maxNameBytes keeps a song's name short enough for a file system, with
// room for the extension, and for the suffix a second copy gains.
const maxNameBytes = 120

// maxNameTries is how many copies of one name a download looks past.
const maxNameTries = 20

// Target is where one download's files go, the lyric and the cover sharing
// the audio's name.
type Target struct {
	Path string
	// Base is the name without the extension.
	Base string
	// Exists says the audio is already there, of the size announced: the
	// download is done already.
	Exists bool
}

// Progress reports how much of the audio has come in, of the whole when the
// service said how long it is, and zero otherwise.
type Progress func(done, total int64)

// downloadAttempts is how long to wait before each try at a song: the first
// comes at once, then two more, as a dropped connection is worth another go
// while a refusal is not.
var downloadAttempts = []time.Duration{0, time.Second, 3 * time.Second}

// DownloadFile fetches the stream into dest, by way of a .part file beside
// it, and keeps it only once all of it has come in. A song that stops short
// is fetched again from the start, so that what is kept is whole.
func DownloadFile(ctx context.Context, stream *pluginapi.Stream, dest string, progress Progress) (int64, error) {
	if stream == nil || stream.URL == "" {
		return 0, pluginapi.ErrUnavailable
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	part := dest + ".part"
	f, err := os.Create(part)
	if err != nil {
		return 0, err
	}
	var n int64
	err = pluginapi.Retry(ctx, downloadAttempts, func(ctx context.Context) error {
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		n, err = fetchStream(ctx, stream, f, progress)
		return err
	})
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil && n == 0 {
		err = fmt.Errorf("%w（没有内容）", ErrDownloadShort)
	}
	if err == nil {
		err = os.Rename(part, dest)
	}
	if err != nil {
		os.Remove(part)
		return n, err
	}
	return n, nil
}

// fetchStream copies the whole body, and refuses one that stops short of
// the length it announced or that never comes at all.
func fetchStream(ctx context.Context, stream *pluginapi.Stream, w io.Writer, progress Progress) (int64, error) {
	source := network.NewMediaSource(stream.URL, stream.Header, stream.Renew, network.BrowserUserAgent)
	res, err := source.Open(ctx, streamClient, http.MethodGet, nil)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, errors.New(res.Status)
	}
	total := res.ContentLength
	if total <= 0 {
		total = stream.Size
	}
	if progress != nil {
		progress(0, total)
	}
	buf := make([]byte, 64<<10)
	var n int64
	for {
		read, err := res.Body.Read(buf)
		if read > 0 {
			if _, writeErr := w.Write(buf[:read]); writeErr != nil {
				return n, writeErr
			}
			n += int64(read)
			if progress != nil {
				progress(n, total)
			}
		}
		if err == io.EOF {
			// A body can end as the service said it would and still be
			// shorter than it announced, which is what the length is for.
			return n, shortBody(res.ContentLength, n)
		}
		if err != nil {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			if short := shortBody(res.ContentLength, n); short != nil {
				return n, short
			}
			return n, err
		}
	}
}

// CopyFile puts a song the cache holds already at dest, by way of the same
// part file a download uses, so that a half copied file is never left as
// the download.
func CopyFile(src, dest string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 0, err
	}
	part := dest + ".part"
	out, err := os.Create(part)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && n == 0 {
		err = fmt.Errorf("%w（没有内容）", ErrDownloadShort)
	}
	if err == nil {
		err = os.Rename(part, dest)
	}
	if err != nil {
		os.Remove(part)
		return n, err
	}
	return n, nil
}

// shortBody describes what came in against what it said it would be, or nil
// when the length was unknown or has all come in.
func shortBody(want, got int64) error {
	if want <= 0 || want == got {
		return nil
	}
	return fmt.Errorf("%w（期望 %d 字节，实际 %d 字节）", ErrDownloadShort, want, got)
}

// ReserveTarget picks the paths one song's files go under in dir: its name,
// cleaned up, with the extension of its format. A file already there of the
// size announced is the song itself; a name another file holds gains a
// number, which its lyric and cover take too.
func ReserveTarget(dir string, t *pluginapi.Track, format string, size int64) (Target, error) {
	if t == nil {
		return Target{}, pluginapi.ErrUnavailable
	}
	if dir == "" {
		return Target{}, errors.New("没有下载目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Target{}, err
	}
	ext := strings.ToLower(strings.TrimPrefix(format, "."))
	if ext == "" {
		ext = "mp3"
	}
	base := SongName(t)
	for try := 1; try <= maxNameTries; try++ {
		name := base
		if try > 1 {
			name = fmt.Sprintf("%s (%d)", base, try)
		}
		path := filepath.Join(dir, name+"."+ext)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return Target{Path: path, Base: name}, nil
		}
		if err != nil {
			return Target{}, err
		}
		if info.Mode().IsRegular() && size > 0 && info.Size() == size {
			return Target{Path: path, Base: name, Exists: true}, nil
		}
	}
	return Target{}, fmt.Errorf("同名文件太多：%s", base)
}

// SongName names a song's files after its artists and then its name,
// without what a file system would refuse or could not hold.
func SongName(t *pluginapi.Track) string {
	name := strings.TrimSpace(t.Name)
	if artists := strings.TrimSpace(t.ArtistNames()); artists != "" {
		name = artists + " - " + name
	}
	if name = sanitizeName(name); name == "" {
		name = "未知歌曲"
	}
	return name
}

// sanitizeName replaces what a file system refuses with an underscore, and
// cuts the name to fit without breaking a rune.
func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.Trim(b.String(), " .")
	if len(out) > maxNameBytes {
		out = out[:maxNameBytes]
		for len(out) > 0 && !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
		out = strings.Trim(out, " .")
	}
	return out
}

// WriteLyrics puts the lyric beside its song as LRC, with each translation
// under the line it answers. An empty lyric is not written at all.
func WriteLyrics(dir, base, lyric, translation string) (string, error) {
	if strings.TrimSpace(lyric) == "" {
		return "", nil
	}
	path := filepath.Join(dir, base+".lrc")
	return path, os.WriteFile(path, []byte(mergeTranslation(lyric, translation)), 0o644)
}

// WriteCover puts the artwork beside its song, under the extension of what
// it is. Anything that is not an image is left out, and what is already
// there is left as the listener has it.
func WriteCover(dir, base string, data []byte) (string, error) {
	ext := imageExtension(data)
	if ext == "" {
		return "", nil
	}
	path := filepath.Join(dir, base+ext)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, os.WriteFile(path, data, 0o644)
}

// imageExtension is the extension the artwork's own bytes ask for.
func imageExtension(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	}
	return ""
}

// lyricLine is one LRC line: its time tags, and the words under them.
type lyricLine struct {
	tags []string
	text string
}

// mergeTranslation puts each translated line under the line of the same
// time, as players of such a file show the two of them together.
func mergeTranslation(lyric, translation string) string {
	if strings.TrimSpace(translation) == "" {
		return lyric
	}
	lines := splitLyric(lyric)
	at := make(map[string]int, len(lines))
	for i, line := range lines {
		if len(line.tags) == 0 || strings.TrimSpace(line.text) == "" {
			continue
		}
		if _, seen := at[line.tags[0]]; !seen {
			at[line.tags[0]] = i
		}
	}
	for _, line := range splitLyric(translation) {
		if len(line.tags) == 0 || strings.TrimSpace(line.text) == "" {
			continue
		}
		if i, ok := at[line.tags[0]]; ok {
			lines[i].text += "\n" + line.text
		}
	}
	var out strings.Builder
	for _, line := range lines {
		out.WriteString(strings.Join(line.tags, ""))
		out.WriteString(line.text)
		out.WriteByte('\n')
	}
	return out.String()
}

// splitLyric reads an LRC into its lines, each with the time tags it starts
// with and the words after them.
func splitLyric(body string) []lyricLine {
	var out []lyricLine
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		raw = strings.TrimRight(raw, "\r")
		if raw == "" {
			continue
		}
		var tags []string
		rest := raw
		for strings.HasPrefix(rest, "[") {
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				break
			}
			tags = append(tags, rest[:end+1])
			rest = rest[end+1:]
		}
		out = append(out, lyricLine{tags: tags, text: rest})
	}
	return out
}
