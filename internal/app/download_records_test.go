package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// A song the listener keeps is remembered where the settings are, is known
// again after a restart, and says so when its file goes away.
func TestDownloadRecordsSurviveRestart(t *testing.T) {
	root := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, root)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })

	body := bytes.Repeat([]byte("ID3"), 512)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	a.loadDownloadRecords()
	inlinePosts(t)
	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	runQueued(a)

	key := downloadRecordKey{platform: "test-platform", ref: "song-ref"}
	if _, ok := a.downloads.records.find(key); !ok {
		t.Fatal("the song that was just kept is not remembered")
	}

	// The host restarts and reads what it kept.
	restarted, _ := downloadApp(downloadAdapter{})
	restarted.settings.DownloadDir = dir
	restarted.downloads = newDownloads()
	restarted.loadDownloadRecords()
	record, ok := restarted.downloads.records.find(key)
	if !ok || record.Level != "standard" || record.Song != "歌名" {
		t.Fatalf("the kept song came back as %+v (%v)", record, ok)
	}
	if record.Size != int64(len(body)) {
		t.Fatalf("the kept song is %d bytes, want %d", record.Size, len(body))
	}
	if list := restarted.downloads.records.list(); len(list) != 1 || list[0].Path != record.Path {
		t.Fatalf("the list holds %+v", list)
	}

	// The file goes: the record stays, and is known to be missing.
	if err := os.Remove(record.Path); err != nil {
		t.Fatal(err)
	}
	restarted.downloads.records.verify()
	if _, ok := restarted.downloads.records.find(key); ok {
		t.Fatal("a record outlived the file it points at")
	}
	if list := restarted.downloads.records.list(); len(list) != 1 || list[0].Path != "" {
		t.Fatalf("the missing record is shown as %+v", list)
	}
}

// A song is not fetched twice: not while it is on its way, and not once the
// listener has it.
func TestDownloadsAreNotFetchedTwice(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 512)
	a, track, _ := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	key := downloadRecordKey{platform: "test-platform", ref: "song-ref"}

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	// Asked for again while the first is still waiting.
	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	if waiting := len(a.downloads.queued); waiting != 1 {
		t.Fatalf("%d songs wait, want the one", waiting)
	}
	if message := latestMessage(t, a); message != downloadAlreadyRunning {
		t.Fatalf("the second ask says %q, want %q", message, downloadAlreadyRunning)
	}

	runQueued(a)
	if _, ok := a.downloads.records.find(key); !ok {
		t.Fatal("the song was fetched but not remembered")
	}

	// Asked for once more, now that it is kept.
	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	if waiting := len(a.downloads.queued); waiting != 0 {
		t.Fatalf("%d songs wait after the song was kept", waiting)
	}
	if message := latestMessage(t, a); message != downloadHeld {
		t.Fatalf("the third ask says %q, want %q", message, downloadHeld)
	}
}

// latestMessage is what the newest download in the list says, whether it is
// waiting its turn or already over.
func latestMessage(t *testing.T, a *app) string {
	t.Helper()
	var newest pluginapi.DownloadItem
	for _, item := range a.downloads.snapshot() {
		if item.ID > newest.ID {
			newest = item
		}
	}
	if newest.ID == 0 {
		t.Fatal("no downloads are listed")
	}
	return newest.Message
}

// Everything waiting is shown, not only the song being fetched.
func TestDownloadsListTheQueue(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 512)
	a, track, _ := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	second := *track
	second.ID, second.Ref, second.Name = 8, "song-ref-2", "另一首"
	if err := a.startDownloads([]pluginapi.Track{*track, second}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	items := a.downloads.snapshot()
	if len(items) != 2 {
		t.Fatalf("%d songs are shown, want both", len(items))
	}
	for _, item := range items {
		if item.Phase != protocol.TaskRunning {
			t.Fatalf("the song %q is %s, want it shown as on its way", item.Name, item.Phase)
		}
	}
}

// A failure can be asked for again, and the listener can clear the list of
// what they have without losing the files.
func TestManagerRetriesAndClears(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 512)
	server := audioServer(t, body)
	a, track, dir := downloadSetup(t, "http://127.0.0.1:1/dead", int64(len(body)), 64)
	healthy := downloadAdapter{streams: func(_ context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		return &pluginapi.Stream{URL: server.URL, Type: "mp3", Level: "standard", Size: int64(len(body))}, nil
	}}
	a.platforms["test-platform"] = downloadAdapter{streams: func(context.Context, *pluginapi.Track, string) (*pluginapi.Stream, error) {
		return nil, pluginapi.ErrUnavailable
	}}

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	runQueued(a)
	items := a.downloads.snapshot()
	if len(items) != 1 || items[0].Phase != protocol.TaskFailed {
		t.Fatalf("the download is %+v, want it failed", items)
	}

	// The platform answers again: the failure is retried.
	a.platforms["test-platform"] = healthy
	if err := a.retryDownload(items[0].ID); err != nil {
		t.Fatalf("retryDownload: %v", err)
	}
	runQueued(a)
	audio := filepath.Join(dir, "歌手 - 歌名.mp3")
	if _, err := os.Stat(audio); err != nil {
		t.Fatalf("the retried song is not there: %v", err)
	}

	// Clearing the list leaves the file alone.
	a.clearDownloadRecords()
	if list := a.downloads.records.list(); len(list) != 0 {
		t.Fatalf("the list still holds %+v", list)
	}
	if _, err := os.Stat(audio); err != nil {
		t.Fatalf("clearing the list removed the file: %v", err)
	}
}

// Asking for a kept song again writes nothing while its file is there, and
// fetches it afresh once it has gone.
func TestManagerRedownloadsAndForgets(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 512)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	key := downloadRecordKey{platform: "test-platform", ref: "song-ref"}
	kept := filepath.Join(dir, "歌手 - 歌名.mp3")

	if err := a.startDownloads([]pluginapi.Track{*track}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	runQueued(a)
	record, ok := a.downloads.records.find(key)
	if !ok {
		t.Fatal("the song was not remembered")
	}

	// The file is there: a second ask is answered with it rather than
	// writing the song again.
	a.redownloadDownload(record)
	runQueued(a)
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the kept file is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "歌手 - 歌名 (2).mp3")); err == nil {
		t.Fatal("a song that is there was written a second time")
	}
	if _, ok := a.downloads.records.find(key); !ok {
		t.Fatal("the second ask did not keep the record")
	}

	// The file goes away: the same ask fetches the song again.
	if err := os.Remove(kept); err != nil {
		t.Fatal(err)
	}
	a.downloads.records.verify()
	a.redownloadDownload(record)
	runQueued(a)
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("a song whose file went missing was not fetched again: %v", err)
	}

	// Forgetting one record leaves the file alone.
	a.forgetDownload(record)
	if list := a.downloads.records.list(); len(list) != 0 {
		t.Fatalf("the list still holds %+v", list)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("forgetting a record removed the file: %v", err)
	}
}

// The manager's cancel stops everything, whichever platform it belongs to.
func TestManagerCancelsEverything(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 512)
	a, track, _ := downloadSetup(t, "http://127.0.0.1:1/dead", int64(len(body)), 64)
	second := *track
	second.ID, second.Ref, second.Name = 8, "song-ref-2", "另一首"
	if err := a.startDownloads([]pluginapi.Track{*track, second}, "standard"); err != nil {
		t.Fatalf("startDownloads: %v", err)
	}
	job, _, cancel := a.downloads.take(a.pluginContext)
	if job == nil {
		t.Fatal("nothing was queued")
	}
	defer cancel()
	a.cancelAllDownloads()
	if a.downloads.running != nil && a.downloads.running.cancel == nil {
		t.Fatal("the song that was running was left without a way to stop")
	}
	if len(a.downloads.queued) != 0 {
		t.Fatalf("%d songs still wait after cancelling everything", len(a.downloads.queued))
	}
}
