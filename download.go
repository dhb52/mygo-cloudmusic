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
// it beside its lyric and cover; QQ Music asks what the song may be
// downloaded as, lets the listener choose, and shows what came in.

// downloadMaxTracks is the most songs one batch may hold, the ceiling the
// host's queue refuses at: a page says so rather than asking for more.
const downloadMaxTracks = 8192

// downloadLevels are the qualities QQ Music serves, from the cheapest up,
// with the words the platform uses for them. higher and exhigh are both
// 320k, so the host answers both with the level it really served, and the
// two are one choice.
var downloadLevels = []struct{ level, label string }{
	{"standard", "标准（128k）"},
	{"higher", "极高（320k）"},
	{"exhigh", "极高（320k）"},
	{"lossless", "无损（FLAC）"},
	{"hires", "Hi-Res（FLAC）"},
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

// downloadLevelOrder is where a level sits among the platform's list,
// unknown ones last.
func downloadLevelOrder(level string) int {
	if rank := downloadLevelRank(level); rank >= 0 {
		return rank
	}
	return len(downloadLevels)
}

// downloadChoice is one quality the dialog offers, as the probed songs
// answered: how many of them serve it, and why none does when none does.
type downloadChoice struct {
	level  string
	format string
	size   int64
	served int // the songs that serve it
	total  int
	reason string // why it is not on offer, when no song serves it
}

// available reports whether any of the songs may be downloaded at it.
func (c downloadChoice) available() bool { return c.served > 0 }

// label is the quality in the platform's own words.
func (c downloadChoice) label() string { return downloadLevelLabel(c.level) }

// detail is what the platform really serves, beside the label: the format,
// the size of one song, and how many songs of a batch it is served for.
func (c downloadChoice) detail() string {
	var parts []string
	if format := strings.ToUpper(c.format); format != "" && !strings.Contains(strings.ToUpper(c.label()), format) {
		parts = append(parts, format)
	}
	if c.total == 1 && c.size > 0 {
		parts = append(parts, megabytes(c.size))
	}
	if c.total > 1 && c.available() {
		parts = append(parts, fmt.Sprintf("%d/%d 首可用", c.served, c.total))
	}
	if !c.available() && c.reason != "" {
		parts = append(parts, c.reason)
	}
	return strings.Join(parts, " · ")
}

// downloadChoices gathers what the probed songs answered into one list, in
// the platform's order, one entry per quality. Two levels the platform
// serves alike, as QQ Music's higher and exhigh, read the same and are one
// choice.
func downloadChoices(answers [][]pluginapi.DownloadOption) []downloadChoice {
	out := make([]downloadChoice, 0, len(downloadLevels))
	answered := 0
	for _, options := range answers {
		// A song whose probe answered nothing at all is not one the batch
		// counts: it said nothing for any quality, so a batch of ten songs
		// one of which answered is a batch of one.
		spoke := false
		for _, o := range options {
			if o.Level == "" {
				continue
			}
			spoke = true
			label := downloadLevelLabel(o.Level)
			i := slices.IndexFunc(out, func(c downloadChoice) bool { return c.label() == label })
			if i < 0 {
				out = append(out, downloadChoice{level: o.Level})
				i = len(out) - 1
			}
			c := &out[i]
			if !o.Available {
				if c.reason == "" {
					c.reason = o.Reason
				}
				continue
			}
			c.served++
			if c.format == "" {
				c.format = o.Format
			}
			if c.size == 0 {
				c.size = o.Size
			}
		}
		if spoke {
			answered++
		}
	}
	for i := range out {
		out[i].total = answered
	}
	slices.SortStableFunc(out, func(a, b downloadChoice) int {
		return downloadLevelOrder(a.level) - downloadLevelOrder(b.level)
	})
	return out
}

// defaultDownloadLevel is the quality the dialog starts on: the one
// remembered, else the platform's own quality setting, else the one on
// offer closest to them. Two as close as each other, the one at or above
// what they wanted is the one they meant.
func defaultDownloadLevel(preferred, quality string, choices []downloadChoice) string {
	for _, wanted := range []string{preferred, quality} {
		if wanted == "" {
			continue
		}
		for _, c := range choices {
			if c.level == wanted && c.available() {
				return c.level
			}
		}
	}
	wanted := quality
	if wanted == "" {
		wanted = preferred
	}
	best := ""
	gap, above := 0, false
	for _, c := range choices {
		if !c.available() {
			continue
		}
		offset := downloadLevelRank(c.level) - downloadLevelRank(wanted)
		diff := offset
		if diff < 0 {
			diff = -diff
		}
		if best == "" || diff < gap || (diff == gap && offset >= 0 && !above) {
			best, gap, above = c.level, diff, offset >= 0
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

// pendingDownload waits for the host's answers about what songs may be
// downloaded as. Probing needs the network, so the host answers later, with
// the token of the request; a batch is asked about one song at a time, as
// the host keeps the answer it gave last.
type pendingDownload struct {
	seq     int
	tracks  []pluginapi.Track
	token   string
	answers [][]pluginapi.DownloadOption
}

// downloadDialogState is the quality dialog: what it offers, what is
// chosen, and what it says about the directory.
type downloadDialogState struct {
	open    bool
	tracks  []pluginapi.Track
	choices []downloadChoice
	level   string
	dir     string
	dirErr  string
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
	if h.downloadPending != nil {
		m.Item("正在检查音质…").Disabled(true)
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
	if h.downloadPending != nil {
		actionPill(c, key, iconDownload, "正在检查音质…", false).Disabled(true)
		return
	}
	if actionPill(c, key, iconDownload, label, false).Clicked() {
		h.beginDownload(tracks)
	}
}

// beginDownload asks what the songs may be downloaded as, and opens the
// quality dialog when the answers are in. A batch longer than the host takes
// is refused here, with the ceiling said.
func (h *Home) beginDownload(tracks []pluginapi.Track) {
	if len(tracks) == 0 {
		return
	}
	// One probe at a time. The host keeps the answer it gave last, so a
	// second request while one is out would bury the first answer under it
	// and leave this one waiting for a token that never comes.
	if h.downloadPending != nil {
		return
	}
	if len(tracks) > downloadMaxTracks {
		h.host.Toast(fmt.Sprintf("一次最多下载 %d 首，请分批下载", downloadMaxTracks))
		return
	}
	// The host is asked about one song at a time, over frames, so the list is
	// kept until the dialog opens: a page that changes under it, a song taken
	// out of the playlist, must not change what is downloaded.
	h.downloadSeq++
	h.downloadPending = &pendingDownload{seq: h.downloadSeq, tracks: slices.Clone(tracks)}
	h.askDownloadOptions()
}

// askDownloadOptions asks about the next song, or opens the dialog once
// every one has answered.
func (h *Home) askDownloadOptions() {
	p := h.downloadPending
	if p == nil {
		return
	}
	if len(p.answers) >= len(p.tracks) {
		h.openDownloadDialog()
		return
	}
	at := len(p.answers)
	p.token = fmt.Sprintf("%s.%d.%d", downloadRef(&p.tracks[at]), at, p.seq)
	h.host.RequestDownloadOptions(p.tracks[at], p.token)
}

// downloadAnswers takes a probe's answer, if it is the one in flight.
func (h *Home) downloadAnswers(state pluginapi.DesktopSettings) {
	p := h.downloadPending
	if p == nil {
		return
	}
	if !state.DownloadAvailable {
		h.downloadPending = nil
		return
	}
	if state.DownloadOptionsToken != p.token {
		return
	}
	p.answers = append(p.answers, state.DownloadOptions)
	h.askDownloadOptions()
}

// openDownloadDialog shows what the songs may be downloaded as.
func (h *Home) openDownloadDialog() {
	p := h.downloadPending
	h.downloadPending = nil
	preferred, quality := "", ""
	if h.preferences != nil {
		preferred, quality = h.preferences.DownloadLevel, h.preferences.Quality
	}
	d := &h.download
	d.tracks, d.choices = p.tracks, downloadChoices(p.answers)
	d.level = defaultDownloadLevel(preferred, quality, d.choices)
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
// on. What the host probed arrives here, keyed on the token of the request.
func (h *Home) downloadDialog(c *ui.Context) {
	state := h.host.DesktopSettings()
	h.downloadAnswers(state)
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
			for i, choice := range d.choices {
				h.downloadChoiceRow(c, i, choice)
			}
			if len(d.choices) == 0 {
				ui.Text(c, "暂时获取不到下载音质，请稍后再试").FontSize(12.5).TextColor(t.TextMuted)
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
			if start.Disabled(d.level == "" || d.dirErr != "").Clicked() {
				h.host.Download(d.tracks, d.level)
				d.open = false
			}
		})
	})
}

// downloadChoiceRow is one quality of the dialog: what it serves, and,
// when the platform does not serve it, why not.
func (h *Home) downloadChoiceRow(c *ui.Context, i int, choice downloadChoice) {
	t := c.Theme()
	d := &h.download
	chosen := choice.level == d.level
	row := ui.ButtonBase(c.Key(fmt.Sprintf("qq-download-level-%d", i))).Padding(10, 12).Gap(10).
		Radius(10).AlignItems(ui.Center).FocusRing(false).Label(choice.label()).Disabled(!choice.available())
	bg := ui.Transparent
	switch {
	case chosen:
		bg = t.Accent.Alpha(0.10)
	case row.Hovered() && choice.available():
		bg = t.Text.Alpha(0.04)
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	row.Children(func() {
		ui.Box(c).Size(16, 16).Center().Children(func() {
			if chosen {
				ui.Icon(c, iconCheck).Size(13, 13).TextColor(t.Accent)
			}
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			name := t.Text
			if chosen {
				name = t.Accent
			}
			ui.Text(c, choice.label()).FontSize(13.5).FontWeight(500).TextColor(name).SingleLine()
			if detail := choice.detail(); detail != "" {
				ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted).SingleLine()
			}
		})
	})
	if row.Clicked() && choice.available() {
		d.level = choice.level
		h.rememberDownloadLevel(choice.level)
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
