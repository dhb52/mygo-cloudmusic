package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// downloadRPC is the host's side of the download actions, reached as a
// platform's process reaches it.
func downloadRPC(t *testing.T, a *app) func(string, PluginHostCall) error {
	t.Helper()
	scope := a.pluginActivations.Prepare("download-test")
	if !a.pluginActivations.Commit(scope) {
		t.Fatal("could not activate the download fixture")
	}
	reverse := pluginReverse{app: a, connection: scope.Connection}
	return func(action string, in PluginHostCall) error {
		in.Scope, in.Action = scope, action
		var reply PluginHostReply
		return reverse.apply(bridge.LocalHostCall(in.Wire()), &reply)
	}
}

// A download asked for over the host's action queues the batch and fetches
// it, which is the whole path a plugin's request takes.
func TestDownloadActionQueuesAndFetches(t *testing.T) {
	inlinePosts(t)
	body := bytes.Repeat([]byte("ID3"), 512)
	a, track, dir := downloadSetup(t, audioServer(t, body).URL, int64(len(body)), 64)
	call := downloadRPC(t, a)

	if err := call(protocol.HostDownload, PluginHostCall{Tracks: []pluginapi.Track{*track}, Level: "standard"}); err != nil {
		t.Fatalf("download: %v", err)
	}
	runQueued(a)

	if got, err := os.ReadFile(filepath.Join(dir, "歌手 - 歌名.mp3")); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("the song is %d bytes (%v), want %d", len(got), err, len(body))
	}
	if items := a.downloads.snapshot(); len(items) != 1 || items[0].Phase != protocol.TaskSucceeded {
		t.Fatalf("the download is %+v", items)
	}
}

// The probe a plugin asks for answers under the token it sent it with.
func TestDownloadOptionsAnswerByToken(t *testing.T) {
	inlinePosts(t)
	a, track := downloadApp(downloadAdapter{streams: streamAtLevels(map[string]string{"standard": "standard"}, 100)})
	a.settings.DownloadDir = t.TempDir()
	call := downloadRPC(t, a)

	if err := call(protocol.HostRequestDownloadOptions, PluginHostCall{Tracks: []pluginapi.Track{*track}, Text: "token-1"}); err != nil {
		t.Fatalf("download-options: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := platformHost{a: a}.DesktopSettings()
		if state.DownloadOptionsToken == "token-1" {
			if len(state.DownloadOptions) == 0 {
				t.Fatal("the token came back with no levels")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the probe never answered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A song of a platform that is not the one asking cannot be probed or
// downloaded through its actions.
func TestDownloadActionsKeepToTheCallingPlatform(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: streamAtLevels(map[string]string{"standard": "standard"}, 100)})
	a.settings.DownloadDir = t.TempDir()
	other := *track
	other.Source = "another-platform"
	call := downloadRPC(t, a)

	for _, action := range []string{protocol.HostRequestDownloadOptions, protocol.HostDownload} {
		in := PluginHostCall{Tracks: []pluginapi.Track{other}, Level: "standard", Text: "token"}
		if err := call(action, in); err == nil {
			t.Fatalf("%s accepted another platform's song", action)
		}
	}
	if err := call(protocol.HostDownload, PluginHostCall{Tracks: []pluginapi.Track{*track, other}, Level: "standard"}); err == nil {
		t.Fatal("download accepted a batch with another platform's song")
	}
	if err := call(protocol.HostDownload, PluginHostCall{Tracks: []pluginapi.Track{*track}, Level: "mystery"}); err != nil {
		t.Fatalf("download refused an unknown level with %v", err)
	}
	if a.toast == "" {
		t.Fatal("an unknown level was refused without saying so")
	}
	// A cancel for something that is not there says so rather than
	// reaching into another platform's downloads.
	a.toast = ""
	if err := call(protocol.HostCancelDownload, PluginHostCall{TaskID: 42}); err != nil {
		t.Fatalf("download-cancel: %v", err)
	}
	if a.toast == "" {
		t.Fatal("a cancel that found nothing said nothing")
	}
}
