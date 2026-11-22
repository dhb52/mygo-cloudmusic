package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"

	"waygu-music/internal/player"
)

// inlinePosts runs what the worker posts straight away, so that a test sees
// the state it leaves without a window.
func inlinePosts(t *testing.T) {
	t.Helper()
	post := postDesktopTask
	postDesktopTask = func(_ *app, fn func()) { fn() }
	t.Cleanup(func() { postDesktopTask = post })
}

// audioServer serves bytes as an audio stream.
func audioServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

// downloadSetup is an app whose one platform serves the test song from url,
// of the size announced.
func downloadSetup(t *testing.T, url string, size int64, cacheMB int) (*app, *pluginapi.Track, string) {
	t.Helper()
	a, track := downloadApp(downloadAdapter{
		streams: func(_ context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
			return &pluginapi.Stream{URL: url, Type: "mp3", Level: "standard", Size: size}, nil
		},
		lyrics: func(context.Context, *pluginapi.Track) (string, string, string, error) {
			return "[00:01.00]词\n", "[00:01.00]word\n", "", nil
		},
		cover: func(context.Context, *pluginapi.Track) ([]byte, error) {
			return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("cover")...), nil
		},
	})
	dir := t.TempDir()
	a.settings.DownloadDir = dir
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.pl.Cache = player.NewCache(t.TempDir(), int64(cacheMB)<<20)
	return a, track, dir
}

// runQueued fetches whatever waits, as the worker does, on this goroutine.
func runQueued(a *app) {
	for {
		job, ctx, cancel := a.downloads.take(a.pluginContext)
		if job == nil {
			return
		}
		a.runDownload(job, ctx, cancel)
	}
}

// A song the listener asks for lands in their download directory with its
// lyric and cover, and the settings window is told how it went.
func TestDownloadsWriteTheSongItsLyricAndItsCover(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 1024)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	if items := a.downloads.snapshot(); len(items) != 1 || items[0].Phase != protocol.TaskRunning {
		t.Fatalf("%d downloads are shown before one is fetched: %+v", len(items), items)
	}
	runQueued(a)

	audio := filepath.Join(dir, "歌手 - 歌名.mp3")
	if got, err := os.ReadFile(audio); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("the song is %d bytes (%v), want %d", len(got), err, len(body))
	}
	if got, err := os.ReadFile(filepath.Join(dir, "歌手 - 歌名.lrc")); err != nil || string(got) != "[00:01.00]词\nword\n" {
		t.Fatalf("the lyric is %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "歌手 - 歌名.jpg")); err != nil {
		t.Fatalf("the cover is missing: %v", err)
	}
	if parts, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(parts) != 0 {
		t.Fatalf("the download left %v", parts)
	}

	items := a.downloads.snapshot()
	if len(items) != 1 || items[0].Phase != protocol.TaskSucceeded || items[0].Path != audio {
		t.Fatalf("the download is %+v, want one that succeeded at %q", items, audio)
	}
	if items[0].Name != "歌手 - 歌名" {
		t.Fatalf("the download is named %q", items[0].Name)
	}
	if task := a.desktopTasks.latest[protocol.TaskSongDownload]; task.Phase != protocol.TaskSucceeded {
		t.Fatalf("the batch task is %+v, want it succeeded", task)
	}
	if a.toast == "" {
		t.Fatal("the listener was told nothing about a finished download")
	}
}

// A song the cache holds already is copied rather than fetched again.
func TestDownloadsCopyWhatTheCacheHolds(t *testing.T) {
	inlinePosts(t)
	a, track, dir := downloadSetup(t, "http://127.0.0.1:1/dead", 0, 64)
	body := bytes.Repeat([]byte("cached"), 512)
	cached := filepath.Join(t.TempDir(), "cached.mp3")
	if err := os.WriteFile(cached, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cachedDownloadFrom(t, cached)

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	runQueued(a)

	got, err := os.ReadFile(filepath.Join(dir, "歌手 - 歌名.mp3"))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("the song is %d bytes (%v), want the %d the cache holds", len(got), err, len(body))
	}
}

// A song already in the download directory, of the size announced, is left
// as it is and called out rather than fetched again.
func TestDownloadsSkipWhatIsThereAlready(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 1024)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	name := "歌手 - 歌名.mp3"
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	runQueued(a)

	items := a.downloads.snapshot()
	if len(items) != 1 || items[0].Phase != protocol.TaskSucceeded || items[0].Message != "已存在" {
		t.Fatalf("the download is %+v, want one already there", items)
	}
	if a.toast == "" {
		t.Fatal("the listener was told nothing about a song that was already there")
	}
}

// Two songs come one after the other, and the batch is summed up for the
// listener when they are done.
func TestDownloadsComeOneAtATimeAndAreSummedUp(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 1024)
	a, track, _ := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	second := *track
	second.ID, second.Ref, second.Name = 8, "song-ref-2", "另一首"

	if err := a.startDownloads([]pluginapi.Track{*track, second}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	if a.downloads.running != nil {
		t.Fatal("a song is being fetched before the worker was asked")
	}
	if len(a.downloads.queued) != 2 {
		t.Fatalf("%d songs wait, want both", len(a.downloads.queued))
	}
	runQueued(a)

	items := a.downloads.snapshot()
	if len(items) != 2 {
		t.Fatalf("%d downloads are shown, want both", len(items))
	}
	for _, item := range items {
		if item.Phase != protocol.TaskSucceeded {
			t.Fatalf("the download of %q went %s: %s", item.Name, item.Phase, item.Message)
		}
	}
	if summary := downloadSummary(downloadTally{succeeded: 2}); a.toast != summary {
		t.Fatalf("the listener was told %q, want the batch summed up as %q", a.toast, summary)
	}
	if task := a.desktopTasks.latest[protocol.TaskSongDownload]; task.Phase != protocol.TaskSucceeded {
		t.Fatalf("the batch task is %+v", task)
	}
	// Nothing about a closed batch is kept.
	if len(a.downloads.left) != 0 || len(a.downloads.tally) != 0 || len(a.downloads.task) != 0 {
		t.Fatalf("the closed batch is still held: %d left, %d tallies, %d tasks",
			len(a.downloads.left), len(a.downloads.tally), len(a.downloads.task))
	}
}

// A song the platform will not serve fails on its own, with the reason, and
// the batch is marked as having failed.
func TestDownloadsReportWhatThePlatformRefuses(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 1024)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	a.platforms["test-platform"] = downloadAdapter{streams: func(_ context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		if level == "standard" {
			return nil, pluginapi.ErrUnavailable
		}
		return nil, pluginapi.ErrUnavailable
	}}

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	runQueued(a)

	items := a.downloads.snapshot()
	if len(items) != 1 || items[0].Phase != protocol.TaskFailed || items[0].Message != downloadNotServed {
		t.Fatalf("the download is %+v, want it failed for the refusal", items)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a failed download left %v", entries)
	}
	if task := a.desktopTasks.latest[protocol.TaskSongDownload]; task.Phase != protocol.TaskFailed {
		t.Fatalf("the batch task is %+v, want it failed", task)
	}
}

// A download the listener stops ends as canceled and leaves nothing.
func TestDownloadsStopWhenCanceled(t *testing.T) {
	inlinePosts(t)
	release := make(chan struct{})
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.Write(bytes.Repeat([]byte("x"), 8<<10))
		w.(http.Flusher).Flush()
		close(started)
		<-release
	}))
	defer server.Close()
	a, track, dir := downloadSetup(t, server.URL, 1<<20, 64)

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	job, ctx, cancel := a.downloads.take(a.pluginContext)
	if job == nil {
		t.Fatal("nothing was queued")
	}
	done := make(chan struct{})
	go func() {
		a.runDownload(job, ctx, cancel)
		close(done)
	}()
	<-started
	if err := a.cancelDownload(job.item.ID, a.platformID); err != nil {
		t.Fatalf("cancelDownload: %v", err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled download did not end")
	}

	items := a.downloads.snapshot()
	if len(items) != 1 || items[0].Phase != protocol.TaskCanceled {
		t.Fatalf("the download is %+v, want it canceled", items)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a canceled download left %v", entries)
	}
	if err := a.cancelDownload(job.item.ID, a.platformID); err == nil {
		t.Fatal("a download that is over was canceled again")
	}
}

// What cannot be downloaded is refused before anything is queued.
func TestStartDownloadsRefusesWhatItCannotDo(t *testing.T) {
	a, track, dir := downloadSetup(t, audioServer(t, []byte("x")).URL, 1, 64)

	if err := a.startDownloads(nil, "standard"); err == nil {
		t.Fatal("a download of nothing was accepted")
	}
	if err := a.startDownloads([]pluginapi.Track{*track}, "mystery"); err == nil {
		t.Fatal("a download at an unknown level was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.settings.DownloadDir = filepath.Join(dir, "file")
	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err == nil || !strings.Contains(err.Error(), "下载目录") {
		t.Fatalf("a download into a file was accepted: %v", err)
	}
	if len(a.downloads.queued) != 0 {
		t.Fatalf("%d songs were queued by a refused download", len(a.downloads.queued))
	}
}

// The download directory is the one chosen, the system's when none was, and
// a path the listener typed is read from their home.
func TestDownloadDirFollowsTheSetting(t *testing.T) {
	a, _ := downloadApp(downloadAdapter{})

	system, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.DownloadDir = ""
	dir, err := a.downloadDir()
	if err != nil || dir != filepath.Clean(want) {
		t.Fatalf("the system download directory came out as %q (%v), want %q", dir, err, want)
	}
	state := platformHost{a: a}.DesktopSettings()
	if state.DownloadDir != dir {
		t.Fatalf("the settings advertise %q, want %q", state.DownloadDir, dir)
	}

	a.settings.DownloadDir = os.TempDir() + string(os.PathSeparator) + "wagyu-downloads"
	if dir, err := a.downloadDir(); err != nil || dir != filepath.Clean(a.settings.DownloadDir) {
		t.Fatalf("the chosen directory came out as %q (%v)", dir, err)
	}
	a.settings.DownloadDir = "~/下载"
	if dir, err := a.downloadDir(); err != nil || dir != filepath.Join(system, "下载") {
		t.Fatalf("a home-relative directory came out as %q (%v)", dir, err)
	}
}

// A preference the plugin writes back keeps the download directory, and a
// settings file from before keeps working without one.
func TestDownloadDirIsKeptInSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"cacheMB":1024,"volume":0.25}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	host := newTestApp()
	host.settings = loadSettings()
	if host.settings.DownloadDir != "" || host.settings.CacheMB != 1024 {
		t.Fatalf("settings from before came back as %+v", host.settings)
	}
	loaded := host.settings
	// What a platform sends back is the whole preference struct, so only
	// the download directory counts as changed here.
	prefs := pluginapi.DesktopPreferences{
		Appearance: loaded.Appearance, AppIcon: loaded.AppIcon, CacheMB: loaded.CacheMB,
		Notch: loaded.Notch, NotchActivity: loaded.NotchActivity, NotchVirtual: loaded.NotchVirtual,
		Smoke: loaded.Smoke, Fog: loaded.Fog,
		UseMPV: loaded.UseMPV, MPVExclusive: loaded.MPVExclusive, MPVGapless: loaded.MPVGapless,
		MPVAudioDevice: loaded.MPVAudioDevice,
		DownloadDir:    "/tmp/music",
	}
	if err := host.applyDesktopPreferences(prefs); err != nil {
		t.Fatalf("applyDesktopPreferences: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"downloadDir": "/tmp/music"`) || !strings.Contains(string(body), `"cacheMB": 1024`) {
		t.Fatalf("the settings file holds\n%s", body)
	}
	if again := loadSettings(); again.DownloadDir != "/tmp/music" || again.CacheMB != 1024 {
		t.Fatalf("the settings came back as %+v", again)
	}
}

// cachedDownloadFrom makes every download copy the file at path, as a cache
// that holds the song would.
func cachedDownloadFrom(t *testing.T, path string) {
	t.Helper()
	previous := cachedDownload
	cachedDownload = func(*player.Cache, pluginapi.ID, *pluginapi.Track, string, string) (string, bool) {
		return path, true
	}
	t.Cleanup(func() { cachedDownload = previous })
}

// deferredPosts collects what the worker posts without running it, as the
// application thread would until it gets to it.
func deferredPosts(t *testing.T) func() {
	t.Helper()
	post := postDesktopTask
	var mu sync.Mutex
	var queued []func()
	postDesktopTask = func(_ *app, fn func()) {
		mu.Lock()
		queued = append(queued, fn)
		mu.Unlock()
	}
	t.Cleanup(func() { postDesktopTask = post })
	return func() {
		for {
			mu.Lock()
			if len(queued) == 0 {
				mu.Unlock()
				return
			}
			fn := queued[0]
			queued = queued[1:]
			mu.Unlock()
			fn()
		}
	}
}

// The queue moves on by itself: the next song of a batch is fetched without
// the interface having run what the one before it posted.
func TestDownloadsAdvanceWithoutTheApplicationThread(t *testing.T) {
	drain := deferredPosts(t)
	body := bytes.Repeat([]byte("ID3"), 1024)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	second := *track
	second.ID, second.Ref, second.Name = 8, "song-ref-2", "另一首"
	if err := a.startDownloads([]pluginapi.Track{*track, second}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}

	for fetched := 1; fetched <= 2; fetched++ {
		job, ctx, cancel := a.downloads.take(a.pluginContext)
		if job == nil {
			t.Fatalf("song %d was never handed to the worker", fetched)
		}
		a.runDownload(job, ctx, cancel)
	}
	names, _ := filepath.Glob(filepath.Join(dir, "*.mp3"))
	if len(names) != 2 {
		t.Fatalf("%d of the two songs landed", len(names))
	}
	if a.downloads.running != nil || len(a.downloads.queued) != 0 {
		t.Fatalf("the worker still holds a song: running=%v, queued=%d", a.downloads.running != nil, len(a.downloads.queued))
	}
	drain()
	if items := a.downloads.snapshot(); len(items) != 2 {
		t.Fatalf("%d downloads are shown, want both", len(items))
	}
}

// A listener who stops a song while the platform is being asked still sees
// it canceled rather than a failure with a reason that is not one.
func TestDownloadsCancelledWhileProbingAreCanceled(t *testing.T) {
	inlinePosts(t)
	a, track, dir := downloadSetup(t, "http://127.0.0.1:1/dead", 0, 64)
	probing, release := make(chan struct{}), make(chan struct{})
	a.platforms["test-platform"] = downloadAdapter{streams: func(ctx context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		select {
		case <-probing:
		default:
			close(probing)
		}
		<-release
		return nil, ctx.Err()
	}}
	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}

	job, ctx, cancel := a.downloads.take(a.pluginContext)
	if job == nil {
		t.Fatal("nothing was queued")
	}
	done := make(chan struct{})
	go func() {
		a.runDownload(job, ctx, cancel)
		close(done)
	}()
	<-probing
	if err := a.cancelDownload(job.item.ID, a.platformID); err != nil {
		t.Fatalf("cancelDownload: %v", err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled download did not end")
	}

	items := a.downloads.snapshot()
	if len(items) != 1 || items[0].Phase != protocol.TaskCanceled || items[0].Message != "" {
		t.Fatalf("the download is %+v, want it canceled without a reason", items)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a canceled download left %v", entries)
	}
}

// Another platform's download cannot be stopped from here.
func TestCancelDownloadKeepsToItsPlatform(t *testing.T) {
	inlinePosts(t)
	a, track, _ := downloadSetup(t, audioServer(t, []byte("x")).URL, 1, 64)
	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	job, _, cancel := a.downloads.take(a.pluginContext)
	if job == nil {
		t.Fatal("nothing was queued")
	}
	defer cancel()
	if err := a.cancelDownload(job.item.ID, "another-platform"); err == nil {
		t.Fatal("another platform canceled this download")
	}
	if job.cancel == nil {
		t.Fatal("the download was left without a way to stop it")
	}
}

// A preference written back without the download directory leaves it alone.
func TestDownloadDirSurvivesAnUnrelatedPreference(t *testing.T) {
	a, _, dir := downloadSetup(t, audioServer(t, []byte("x")).URL, 1, 64)
	if a.settings.DownloadDir != dir {
		t.Fatalf("the download directory is %q, want %q", a.settings.DownloadDir, dir)
	}
	// What a plugin sends back is the preferences the host gave it.
	prefs := platformHost{a: a}.DesktopSettings().Preferences
	prefs.Notch = !prefs.Notch
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	if err := a.applyDesktopPreferences(prefs); err != nil {
		t.Fatalf("applyDesktopPreferences: %v", err)
	}
	if a.settings.DownloadDir != dir {
		t.Fatalf("an unrelated preference left the download directory as %q", a.settings.DownloadDir)
	}
}
