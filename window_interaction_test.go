package qqmusic

import (
	"github.com/waygu-music-plugin/sdk/plugintest"
	"testing"
)

// Font and lock controls affect only an unlocked desktop lyrics window under the pointer.
func TestDesktopLyricsToolbar(t *testing.T) {
	plugintest.CheckDesktopLyricsToolbar(t, plugintest.Start(t, Registration()))
}

// Activating the plugin again resets navigation to its landing page.
func TestActivationRestoresLandingPage(t *testing.T) {
	entry := Registration()
	h := plugintest.NewHost(entry.ID)
	adapter := h.Start(entry, nil).(*Adapter)
	h.Activate(t)
	adapter.Home().Open(Route{Page: PageSearch, Query: "example"})
	if got := adapter.Home().Route(); got.Page != PageSearch {
		t.Fatalf("plugin did not navigate to search: %+v", got)
	}
	h.Lifecycle.Deactivate(true)
	h.Activate(t)
	if got := adapter.Home().Route(); got.Page != PageHome {
		t.Fatalf("activated with route %+v", got)
	}
}
