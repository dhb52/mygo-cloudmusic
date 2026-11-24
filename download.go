package qqmusic

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// A download is a song the listener keeps. The host fetches it and writes
// it beside its lyric and cover; QQ Music lists the qualities it serves,
// lets the listener choose, and the host records what the platform served.

// downloadMaxTracks is the most songs one batch may hold, the ceiling the
// host's queue refuses at: a page says so rather than asking for more.
const downloadMaxTracks = 8192

// downloadLevels are the qualities QQ Music serves, from the cheapest up,
// with the words the platform uses for them, and whether the download
// dialog offers them. higher and exhigh are the one 320k request, so the
// dialog lists one and the host records whichever the platform served;
// Hi-Res has no request of its own.
var downloadLevels = []struct {
	level, label string
	dialog       bool
}{
	{"standard", "标准（128k）", true},
	{"higher", "极高（320k）", true},
	{"exhigh", "极高（320k）", false},
	{"lossless", "无损（FLAC）", true},
	{"hires", "Hi-Res（FLAC）", false},
}

// downloadLevelOffered is whether the download dialog lists the quality.
func downloadLevelOffered(level string) bool {
	for _, known := range downloadLevels {
		if known.level == level {
			return known.dialog
		}
	}
	return false
}

// downloadLevelLabel is what the platform calls one level.
func downloadLevelLabel(level string) string {
	for _, known := range downloadLevels {
		if known.level == level {
			return known.label
		}
	}
	return level
}

// downloadLevelRank orders the levels by what they serve, so that the one
// closest to what the listener asked for can be found.
func downloadLevelRank(level string) int {
	for i, known := range downloadLevels {
		if known.level == level {
			return i
		}
	}
	return -1
}

// downloadLevelLabels are what the platform calls each of these levels, in
// the order they were given: the one table of words both the download
// dialog and the settings window's 音质 row read.
func downloadLevelLabels(levels []string) []string {
	labels := make([]string, len(levels))
	for i, level := range levels {
		labels[i] = downloadLevelLabel(level)
	}
	return labels
}

// defaultDownloadLevel is the quality the dialog starts on: the one
// remembered when the dialog offers it, else the platform's own quality
// setting, else the level closest to it. Two as close as each other, the
// one at or above what they wanted is the one they meant.
func defaultDownloadLevel(preferred, quality string) string {
	if downloadLevelOffered(preferred) {
		return preferred
	}
	// higher and exhigh are the one 320k request; the dialog lists higher.
	if quality == "exhigh" {
		quality = "higher"
	}
	best := ""
	gap, above := 0, false
	for i, known := range downloadLevels {
		if !known.dialog {
			continue
		}
		offset := i - downloadLevelRank(quality)
		diff := offset
		if diff < 0 {
			diff = -diff
		}
		if best == "" || diff < gap || (diff == gap && offset >= 0 && !above) {
			best, gap, above = known.level, diff, offset >= 0
		}
	}
	return best
}

// downloadDirWritable makes the download directory when it is not there,
// and says whether a song may be written in it: a directory nothing can
// be written to is worth saying before the listener starts.
func downloadDirWritable(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("宿主还没有给出下载目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("下载目录不可用：%w", err)
	}
	f, err := os.CreateTemp(dir, ".wagyu-download-*")
	if err != nil {
		return fmt.Errorf("下载目录不可写：%w", err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

// downloading reports whether that song is coming in now. The host names a
// download for the file it writes, which cleans the song's words for the
// file system; the song's own reference is what the two are matched on.
func downloading(state pluginapi.DesktopSettings, tr *pluginapi.Track) bool {
	ref := downloadRef(tr)
	if ref == "" {
		return false
	}
	return slices.ContainsFunc(state.Downloads, func(item pluginapi.DownloadItem) bool {
		return item.Phase == protocol.TaskRunning && item.Ref == ref
	})
}

// downloadRunning reports whether a batch is coming in now, which is when a
// page waits rather than queueing its list again: the host takes one batch at
// a time, and never names the batch a page started, so a song coming in is
// that batch.
func downloadRunning(state pluginapi.DesktopSettings) bool {
	return slices.ContainsFunc(state.Downloads, func(item pluginapi.DownloadItem) bool {
		return item.Phase == protocol.TaskRunning
	})
}

// downloadRef names one song to the host: its own reference where it has
// one.
func downloadRef(tr *pluginapi.Track) string {
	if tr == nil {
		return ""
	}
	if tr.Ref != "" {
		return tr.Ref
	}
	return strconv.FormatInt(tr.ID, 10)
}

// downloadDialogState is the quality dialog: the batch it is over, what is
// chosen, and what it says about the directory.
type downloadDialogState struct {
	open   bool
	tracks []pluginapi.Track
	level  string
	dir    string
	dirErr string
	// dirChecked is whether the directory has been looked at since the
	// dialog opened: the first frame checks it too, so that a host that
	// never settled on one is said rather than the download starting into
	// nowhere.
	dirChecked bool
}

// downloadMenuItem adds the song's download entry, which only a host that
// downloads songs has: a song already coming in says so and stays out of
// reach.
func (h *Home) downloadMenuItem(m *ui.Menu, tr *pluginapi.Track) {
	if tr == nil {
		return
	}
	state := h.host.DesktopSettings()
	if !state.DownloadAvailable {
		return
	}
	m.Separator()
	if downloading(state, tr) {
		m.Item("下载中…").Disabled(true)
		return
	}
	if m.Item("下载").Chosen() {
		h.beginDownload([]pluginapi.Track{*tr})
	}
}

// downloadEntryShown is whether a page's download entry belongs on it at all:
// a host that downloads songs, and a list with something in it.
func downloadEntryShown(state pluginapi.DesktopSettings, count int) bool {
	return state.DownloadAvailable && count > 0
}

// pageDownloadEntry draws a page's own download entry, which hands the
// page's whole list to the quality dialog: an album's songs, a playlist's,
// the queue. It only shows for a host that downloads songs and a list with
// something in it; while a batch is coming in it says so and stays out of
// reach, rather than queueing the same songs again.
func (h *Home) pageDownloadEntry(c *ui.Context, key, label string, tracks []pluginapi.Track) {
	state := h.host.DesktopSettings()
	if !downloadEntryShown(state, len(tracks)) {
		return
	}
	if downloadRunning(state) {
		actionPill(c, key, iconDownload, "下载中…", false).Disabled(true)
		return
	}
	if actionPill(c, key, iconDownload, label, false).Clicked() {
		h.beginDownload(tracks)
	}
}

// beginDownload opens the quality dialog on the levels the plugin lists,
// refusing a batch longer than the host takes with the ceiling said.
func (h *Home) beginDownload(tracks []pluginapi.Track) {
	if len(tracks) == 0 {
		return
	}
	if len(tracks) > downloadMaxTracks {
		h.host.Toast(fmt.Sprintf("一次最多下载 %d 首，请分批下载", downloadMaxTracks))
		return
	}
	preferred, quality := "", ""
	if h.preferences != nil {
		preferred, quality = h.preferences.DownloadLevel, h.preferences.Quality
	}
	// The list is kept: a page that changes under the dialog, a song taken
	// out of the playlist, must not change what is downloaded.
	d := &h.download
	d.tracks = slices.Clone(tracks)
	d.level = defaultDownloadLevel(preferred, quality)
	d.dir, d.dirErr, d.dirChecked = "", "", false
	d.open = true
}

// rememberDownloadLevel keeps the quality the listener chose for the next
// dialog.
func (h *Home) rememberDownloadLevel(level string) {
	if h.preferences == nil || h.preferences.DownloadLevel == level {
		return
	}
	h.preferences.DownloadLevel = level
	if h.savePreferences != nil {
		h.savePreferences()
	}
}

// setDownloadDir points downloads somewhere else, leaving every other
// preference as it was.
func (h *Home) setDownloadDir(dir string) {
	preferences := h.host.DesktopSettings().Preferences
	if preferences.DownloadDir == dir {
		return
	}
	preferences.DownloadDir = dir
	h.host.ApplyDesktopPreferences(preferences)
	h.host.Invalidate()
}

// chooseDownloadDir asks for the directory downloads go to. The system's
// picker blocks until it is dismissed, so it runs off the interface, on the
// scope and the directory it was opened with: the listener may move on
// while it is up.
func (h *Home) chooseDownloadDir() {
	ctx, dir := h.scope, h.host.DesktopSettings().DownloadDir
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title: "选择下载目录", Directory: true, CreateDirectories: true,
			DefaultPath: dir,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		h.postResult(ctx, func() { h.setDownloadDir(paths[0]) })
	}()
}

// openDownloadDir shows the directory downloads go to.
func (h *Home) openDownloadDir(dir string) {
	if dir == "" {
		return
	}
	if err := mygo.Shell.OpenPath(dir); err != nil {
		h.host.Toast("打不开下载目录")
	}
}

// downloadDialog is the quality dialog, over the page the song was picked
// on: the levels the platform serves, and where the songs go.
func (h *Home) downloadDialog(c *ui.Context) {
	state := h.host.DesktopSettings()
	d := &h.download
	if !d.open {
		return
	}
	t := c.Theme()
	if !d.dirChecked || d.dir != state.DownloadDir {
		d.dir, d.dirErr, d.dirChecked = state.DownloadDir, "", true
		if err := downloadDirWritable(d.dir); err != nil {
			d.dirErr = err.Error()
		}
	}
	ui.DialogBase(c.Key("qq-download"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(420).Padding(24).Radius(18).Gap(14).Background(t.Background).Border(1, t.Border).
			Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4))
		title := "下载"
		if len(d.tracks) == 1 {
			title = "下载「" + d.tracks[0].Name + "」"
		}
		ui.Text(c, title).FontSize(17).FontWeight(700)
		ui.Column(c).Gap(2).Children(func() {
			for i, known := range downloadLevels {
				if !known.dialog {
					continue
				}
				h.downloadChoiceRow(c, i, known.level)
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconFolder).Size(13, 13).TextColor(t.TextMuted)
			ui.Text(c, state.DownloadDir).FontSize(12.5).TextColor(t.TextMuted).Grow(1).Shrink(1).MinWidth(0).SingleLine()
			if ui.Button(c.Key("qq-download-dir"), "更改…").Clicked() {
				h.chooseDownloadDir()
			}
			if ui.Button(c.Key("qq-download-dir-open"), "打开").Clicked() {
				h.openDownloadDir(state.DownloadDir)
			}
			if ui.Button(c.Key("qq-download-dir-default"), "恢复默认").
				Disabled(state.Preferences.DownloadDir == "").Clicked() {
				h.setDownloadDir("")
			}
		})
		if d.dirErr != "" {
			ui.Text(c, d.dirErr).FontSize(12).TextColor(t.Accent).LineHeight(1.4)
		}
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if actionPill(c, "qq-download-cancel", nil, "取消", false).Clicked() {
				d.open = false
			}
			start := actionPill(c, "qq-download-start", nil, "开始下载", true)
			if start.Disabled(d.dirErr != "").Clicked() {
				h.host.Download(d.tracks, d.level)
				d.open = false
			}
		})
	})
}

// downloadChoiceRow is one quality of the dialog: its label, and whether it
// is the one chosen.
func (h *Home) downloadChoiceRow(c *ui.Context, i int, level string) {
	t := c.Theme()
	d := &h.download
	chosen := level == d.level
	label := downloadLevelLabel(level)
	row := ui.ButtonBase(c.Key(fmt.Sprintf("qq-download-level-%d", i))).Padding(10, 12).Gap(10).
		Radius(10).AlignItems(ui.Center).FocusRing(false).Label(label)
	bg := ui.Transparent
	switch {
	case chosen:
		bg = t.Accent.Alpha(0.10)
	case row.Hovered():
		bg = t.Text.Alpha(0.04)
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	row.Children(func() {
		ui.Box(c).Size(16, 16).Center().Children(func() {
			if chosen {
				ui.Icon(c, iconCheck).Size(13, 13).TextColor(t.Accent)
			}
		})
		name := t.Text
		if chosen {
			name = t.Accent
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(name).Grow(1).Shrink(1).MinWidth(0).SingleLine()
	})
	if row.Clicked() {
		d.level = level
		h.rememberDownloadLevel(level)
	}
}

// downloadProgress says how far one song has come.
func downloadProgress(item pluginapi.DownloadItem) string {
	switch {
	case item.Total > 0:
		return fmt.Sprintf("%d%%（%s / %s）", item.Done*100/item.Total, megabytes(item.Done), megabytes(item.Total))
	case item.Done > 0:
		return "已下载 " + megabytes(item.Done)
	default:
		return "准备中…"
	}
}
