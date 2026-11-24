package desktop

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/waygu-music-plugin/sdk/pluginapi"
)

// ParseLyricsColor accepts an opaque RRGGBB color with a leading hash.
func ParseLyricsColor(value string) (ui.Color, bool) {
	value, ok := strings.CutPrefix(value, "#")
	if !ok || len(value) != 6 {
		return ui.Color{}, false
	}
	bytes, err := hex.DecodeString(value)
	if err != nil {
		return ui.Color{}, false
	}
	return ui.RGB(bytes[0], bytes[1], bytes[2]), true
}

// LyricsColor returns the custom color when it is set.
func LyricsColor(value string, fallback ui.Color) ui.Color {
	if color, ok := ParseLyricsColor(value); ok {
		return color
	}
	return fallback
}

type lyricsColorDraft struct {
	saved ui.Color
	color ui.Color
}

// LyricsColorControl lets the user preview a color before writing settings.
func LyricsColorControl(c *ui.Context, host pluginapi.Services, state pluginapi.DesktopSettings, preset ui.Color) {
	settings, ok := host.(pluginapi.LyricsSettingsServices)
	if !ok || !state.DesktopLyricsColorAvailable {
		return
	}
	preset.A = 255
	saved := LyricsColor(state.DesktopLyricsColor, preset)
	row := ui.Row(c).Gap(8).AlignItems(ui.Center)
	draft := ui.Local(row, "color", func() lyricsColorDraft {
		return lyricsColorDraft{saved: saved, color: saved}
	})
	if draft.saved != saved {
		*draft = lyricsColorDraft{saved: saved, color: saved}
	}
	row.Children(func() {
		ui.ColorWell(c.Key("picker"), &draft.color).Label("自定义桌面歌词颜色")
		draft.color.A = 255
		if ui.Button(c.Key("apply"), "应用").Disabled(draft.color == saved).Clicked() {
			settings.SetDesktopLyricsColor(fmt.Sprintf("#%02x%02x%02x", draft.color.R, draft.color.G, draft.color.B))
		}
		if ui.Button(c.Key("reset"), "恢复预设").Disabled(state.DesktopLyricsColor == "").Clicked() {
			settings.SetDesktopLyricsColor("")
		}
	})
}

// StatusLyricsControl reflects the host-owned taskbar lyrics setting.
func StatusLyricsControl(c *ui.Context, host pluginapi.Services, state pluginapi.DesktopSettings) {
	settings, ok := host.(pluginapi.LyricsSettingsServices)
	if !ok || !state.StatusLyricsAvailable {
		return
	}
	enabled := state.StatusLyricsEnabled
	if ui.Switch(c, &enabled).Label("任务栏歌词").Changed() {
		settings.SetStatusLyrics(enabled)
	}
}

// TaskbarLyricsFontControl adjusts the host's resizable taskbar lyrics font.
func TaskbarLyricsFontControl(c *ui.Context, host pluginapi.Services, state pluginapi.DesktopSettings) {
	settings, ok := host.(pluginapi.TaskbarLyricsFontServices)
	if !ok || state.TaskbarLyricsFontSize == 0 {
		return
	}
	size := float64(state.TaskbarLyricsFontSize)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.StepSlider(c.Key("slider"), &size, pluginapi.TaskbarLyricsMinFontSize, pluginapi.TaskbarLyricsMaxFontSize, 1).
			Width(120).Label("任务栏歌词字号").Changed() {
			settings.SetTaskbarLyricsFontSize(int(size))
		}
		ui.Text(c.Key("size"), strconv.Itoa(int(size))).SingleLine()
	})
}

// TaskbarLyricsFontFamilyControl lets the listener choose an installed font.
func TaskbarLyricsFontFamilyControl(c *ui.Context, host pluginapi.Services, state pluginapi.DesktopSettings) {
	settings, ok := host.(pluginapi.TaskbarLyricsFontServices)
	if !ok || state.TaskbarLyricsFontSize == 0 {
		return
	}
	lyricsFontFamilyControl(c, state.TaskbarLyricsFontFamily, "选择任务栏歌词字体", settings.SetTaskbarLyricsFontFamily)
}

// LyricsFontFamilyControl picks the shared font for the app's lyric surfaces.
func LyricsFontFamilyControl(c *ui.Context, host pluginapi.Services, state pluginapi.DesktopSettings) {
	settings, ok := host.(pluginapi.LyricsFontServices)
	if !ok || !state.LyricsFontAvailable {
		return
	}
	lyricsFontFamilyControl(c, state.LyricsFontFamily, "选择歌词字体", settings.SetLyricsFontFamily)
}

func lyricsFontFamilyControl(c *ui.Context, current, label string, set func(string)) {
	const defaultFont = "跟随默认"
	families := ui.FontFamilies()
	options := make([]string, 1, len(families)+2)
	options[0] = defaultFont
	selected := defaultFont
	for _, family := range families {
		if family.Registered {
			continue
		}
		options = append(options, family.Name)
		if family.Name == current {
			selected = family.Name
		}
	}
	if selected == defaultFont && current != "" {
		selected = current + "（不可用）"
		options = append(options, selected)
	}
	if ui.Select(c, &selected, options).Width(170).Label(label).Changed() {
		if selected == defaultFont {
			selected = ""
		}
		set(selected)
	}
}
