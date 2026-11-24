package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/waygu-music-plugin/sdk/pluginapi"
)

type lyricsSettingsHost struct {
	pluginapi.Services
	state       pluginapi.DesktopSettings
	colorWrites int
}

func (h *lyricsSettingsHost) SetDesktopLyricsColor(color string) {
	h.colorWrites++
	h.state.DesktopLyricsColor = color
}

func (h *lyricsSettingsHost) SetStatusLyrics(enabled bool) {
	h.state.StatusLyricsEnabled = enabled
}

func (h *lyricsSettingsHost) SetTaskbarLyricsFontSize(size int) {
	h.state.TaskbarLyricsFontSize = size
}

func (h *lyricsSettingsHost) SetTaskbarLyricsFontFamily(family string) {
	h.state.TaskbarLyricsFontFamily = family
}

func (h *lyricsSettingsHost) SetLyricsFontFamily(family string) {
	h.state.LyricsFontFamily = family
}

// Choosing a swatch only edits the draft until Apply, and resetting clears the override.
func TestLyricsSettingsApplyAndResetColor(t *testing.T) {
	host := &lyricsSettingsHost{state: pluginapi.DesktopSettings{DesktopLyricsColorAvailable: true}}
	tester := ui.NewTester(func(c *ui.Context) {
		LyricsColorControl(c, host, host.state, ui.RGB(255, 116, 80))
	}, 500, 500)
	click := func(label string) {
		t.Helper()
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("自定义桌面歌词颜色")
	click("Green")
	if host.colorWrites != 0 {
		t.Fatalf("choosing a swatch saved settings: %q", host.state.DesktopLyricsColor)
	}
	tester.Key(0, ui.KeyEscape)
	click("应用")
	if host.colorWrites != 1 || host.state.DesktopLyricsColor != "#22c55e" {
		t.Fatalf("applying the selected color saved %q after %d writes", host.state.DesktopLyricsColor, host.colorWrites)
	}
	click("恢复预设")
	if host.colorWrites != 2 || host.state.DesktopLyricsColor != "" {
		t.Fatalf("restoring the preset saved %q after %d writes", host.state.DesktopLyricsColor, host.colorWrites)
	}
}

// A keyboard adjustment to the taskbar lyrics slider updates the host's font size.
func TestTaskbarLyricsFontControlChangesSize(t *testing.T) {
	host := &lyricsSettingsHost{state: pluginapi.DesktopSettings{
		TaskbarLyricsFontSize: pluginapi.TaskbarLyricsDefaultFontSize,
	}}
	tester := ui.NewTester(func(c *ui.Context) {
		TaskbarLyricsFontControl(c, host, host.state)
	}, 300, 100)
	tester.Key(0, ui.KeyTab)
	tester.Key(0, ui.KeyRight)
	if got := host.state.TaskbarLyricsFontSize; got != pluginapi.TaskbarLyricsDefaultFontSize+1 {
		t.Fatalf("the host received size %d", got)
	}
}

// Choosing the shared lyric font writes the family to the host, and the default clears it.
func TestLyricsFontFamilyControlChangesFamily(t *testing.T) {
	var family string
	for _, installed := range ui.FontFamilies() {
		if !installed.Registered {
			family = installed.Name
			break
		}
	}
	if family == "" {
		t.Skip("no system fonts available")
	}
	host := &lyricsSettingsHost{state: pluginapi.DesktopSettings{
		LyricsFontAvailable: true,
	}}
	tester := ui.NewTester(func(c *ui.Context) {
		LyricsFontFamilyControl(c, host, host.state)
	}, 320, 500)
	for _, choice := range []struct{ label, family string }{
		{family, family},
		{"跟随默认", ""},
	} {
		if err := tester.Click("选择歌词字体"); err != nil {
			t.Fatal(err)
		}
		if err := tester.Click(choice.label); err != nil {
			t.Fatal(err)
		}
		if got := host.state.LyricsFontFamily; got != choice.family {
			t.Fatalf("the host received family %q, want %q", got, choice.family)
		}
	}
}
