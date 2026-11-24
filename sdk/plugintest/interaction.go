package plugintest

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/waygu-music-plugin/sdk/pluginapi"
)

func CheckSpaceAfterOpeningNowPlaying(t *testing.T, h *Host) {
	t.Helper()
	tr := pluginapi.Track{ID: 7, Name: "宝船", Artists: []pluginapi.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	h.Engine.Cue([]pluginapi.Track{tr})
	h.Engine.SetLyrics(nil)
	h.Engine.Hold(0)

	tt := ui.NewTester(h.Windows[pluginapi.MainWindow].Root, 1200, 800)
	if err := tt.Click("打开正在播放"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if _, ok := tt.Find("关闭"); !ok {
		t.Fatal("now playing did not open")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if h.Engine.Playing() {
		t.Error("Space did not pause")
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if !h.Engine.Playing() {
		t.Error("Space did not play again")
	}
}

func CheckScrubSeeksOnRelease(t *testing.T, h *Host) {
	t.Helper()
	tr := pluginapi.Track{ID: 7, Name: "宝船", Duration: 200000}
	h.Engine.Cue([]pluginapi.Track{tr, {ID: 8, Name: "下一首"}})
	h.Engine.SetLyrics(nil)
	h.Engine.Hold(0)
	tt := ui.NewTester(h.Windows[pluginapi.MainWindow].Root, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	bar, ok := tt.Find("播放进度")
	if !ok {
		t.Fatal("no progress bar")
	}
	y := bar.Y + bar.H/2
	at := func(f float32) float32 { return bar.X + bar.W*f }

	tt.Press(at(0.25), y)
	tt.Frame()
	tt.Move(at(0.6), y-40) // off the bar, above it
	tt.Frame()
	tt.Move(at(1)+30, y+30) // past its end, below it
	tt.Frame()
	if pos := h.Engine.Position(); pos > 10*time.Second {
		t.Fatalf("seeked to %v while dragging", pos)
	}
	if h.Engine.Current().ID != 7 {
		t.Fatal("went to the next song while dragging")
	}
	tt.Move(at(0.6), y-40)
	tt.Frame()
	tt.Release(at(0.6), y-40)
	tt.Frame()
	tt.Frame()
	if pos := h.Engine.Position(); pos < 110*time.Second || pos > 130*time.Second {
		t.Errorf("let go at 60%%, at %v", pos)
	}
}

func CheckDesktopLyricsToolbar(t *testing.T, h *Host) {
	t.Helper()
	h.Lyrics = pluginapi.DesktopLyricsState{PointerInside: true, FontSize: 30}
	window := h.Windows[pluginapi.DesktopLyricsWindow]
	tt := ui.NewTester(window.Root, window.Options.Width, window.Options.Height)
	tt.Frame()
	plus, ok := tt.Find("增大字号")
	if !ok {
		t.Fatal("the font control is unavailable under the pointer")
	}
	if err := tt.Click("增大字号"); err != nil {
		t.Fatal(err)
	}
	if h.Lyrics.FontSize != 32 {
		t.Fatal("increasing the font size did not update lyrics preferences")
	}
	if err := tt.Click("锁定桌面歌词"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.ClickAt(plus.X+plus.W/2, plus.Y+plus.H/2)
	if !h.Lyrics.Locked || h.Lyrics.FontSize != 32 {
		t.Fatal("the old toolbar location accepted input while the lyrics were locked")
	}
	h.Lyrics.Locked, h.Lyrics.PointerInside = false, false
	tt.Frame()
	tt.ClickAt(plus.X+plus.W/2, plus.Y+plus.H/2)
	if h.Lyrics.FontSize != 32 {
		t.Fatal("the toolbar accepted input outside of the lyrics window")
	}
}
