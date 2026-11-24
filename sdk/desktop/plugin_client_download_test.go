package desktop

import (
	"context"
	"testing"

	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// Download calls queue without a window and without waiting for the host.
func TestPluginClientDownloadCallsQueue(t *testing.T) {
	services := &pluginClientServices{
		child:    &pluginChild{ctx: context.Background()},
		commands: make(chan bridge.PluginHostCall, 4),
	}
	services.Download([]pluginapi.Track{{Name: "歌", ID: 5}}, "lossless")
	services.CancelDownload(7)
	services.OpenDownloads()

	take := func() bridge.PluginHostCall {
		t.Helper()
		select {
		case cmd := <-services.commands:
			return cmd
		default:
			t.Fatal("a download call did not queue")
			return bridge.PluginHostCall{}
		}
	}
	download := take()
	if download.Action != protocol.HostDownload || download.Level != "lossless" || download.Text != "" ||
		len(download.Tracks) != 1 || download.Tracks[0].ID != 5 {
		t.Fatalf("unexpected download call: %#v", download)
	}
	cancel := take()
	if cancel.Action != protocol.HostCancelDownload || cancel.TaskID != 7 {
		t.Fatalf("unexpected cancel call: %#v", cancel)
	}
	manager := take()
	if manager.Action != protocol.HostDownloadManager {
		t.Fatalf("unexpected manager call: %#v", manager)
	}
}
