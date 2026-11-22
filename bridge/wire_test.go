package bridge

import (
	"reflect"
	"testing"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// Preferences and desktop settings keep their download state across the wire.
func TestDesktopDownloadWireRoundTrip(t *testing.T) {
	prefs := pluginapi.DesktopPreferences{
		Appearance: "dark", CacheMB: 512, NotchActivity: true, MPVAudioDevice: "coreaudio",
		DownloadDir: "/Users/someone/Music",
	}
	if got := LocalDesktopPreferences(WireDesktopPreferences(prefs)); got != prefs {
		t.Fatalf("preferences did not round trip: %#v", got)
	}

	desktop := pluginapi.DesktopSettings{
		Preferences: prefs, DownloadAvailable: true, DownloadDir: "/Users/someone/Music",
		DownloadOptionsToken: "probe-1",
		Downloads: []pluginapi.DownloadItem{{
			ID: 7, Batch: 3, Name: "歌手 - 歌名", Ref: "song-ref", Level: "lossless", Phase: "running",
			Done: 1024, Total: 4096, Path: "/Users/someone/Music/歌手 - 歌名.flac",
		}},
		DownloadOptions: []pluginapi.DownloadOption{
			{Level: "standard", Format: "mp3", Size: 3_500_000, Available: true},
			{Level: "lossless", Format: "flac", Size: 32_400_000, Reason: "需要会员"},
		},
	}
	facade := PluginSnapshot{Desktop: desktop}

	wire := WireSnapshot(facade).Desktop
	if !wire.DownloadAvailable || wire.DownloadDir != desktop.DownloadDir || wire.DownloadOptionsToken != "probe-1" {
		t.Fatalf("availability and directory did not reach the wire: %#v", wire)
	}
	if wire.Preferences.DownloadDir != prefs.DownloadDir {
		t.Fatalf("wire preferences kept %q", wire.Preferences.DownloadDir)
	}
	if len(wire.Downloads) != 1 {
		t.Fatalf("wire carried %d downloads", len(wire.Downloads))
	}
	item := wire.Downloads[0]
	if item.ID != 7 || item.Batch != 3 || item.Name != "歌手 - 歌名" || item.Level != "lossless" ||
		item.Phase != "running" || item.Done != 1024 || item.Total != 4096 || item.Path != desktop.Downloads[0].Path {
		t.Fatalf("wire lost download state: %#v", item)
	}
	if len(wire.DownloadOptions) != 2 {
		t.Fatalf("wire carried %d options", len(wire.DownloadOptions))
	}
	option := wire.DownloadOptions[1]
	if option.Level != "lossless" || option.Format != "flac" || option.Size != 32_400_000 || option.Available || option.Reason != "需要会员" {
		t.Fatalf("wire lost option state: %#v", option)
	}

	if got := LocalSnapshot(WireSnapshot(facade)).Desktop; !reflect.DeepEqual(got, desktop) {
		t.Fatalf("desktop settings did not round trip:\n%#v\n%#v", got, desktop)
	}
	if wire.Downloads[0].Phase != protocol.TaskPhase("running") {
		t.Fatalf("wire phase is %q", wire.Downloads[0].Phase)
	}

	call := PluginHostCall{
		Action: protocol.HostDownload, Tracks: []pluginapi.Track{{Name: "歌", ID: 5}},
		Level: "lossless", Text: "probe", TaskID: 7,
	}
	back := LocalHostCall(call.Wire())
	if back.Level != "lossless" || back.Text != "probe" || back.TaskID != 7 || back.Tracks[0].ID != 5 {
		t.Fatalf("host call lost its download arguments: %#v", back)
	}
}
