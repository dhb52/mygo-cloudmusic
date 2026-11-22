package qqmusic

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	backend "github.com/waygu-music-plugin/qqmusic/internal/client"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/plugintest"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// downloadTestHost answers the download calls as a host does: a probe is
// answered later, with the token it came with.
type downloadTestHost struct {
	Host
	desktop    pluginapi.DesktopSettings
	playback   pluginapi.PlaybackControls
	probe      func(tr pluginapi.Track) []pluginapi.DownloadOption
	requested  []pluginapi.Track
	asked      []string
	downloaded []pluginapi.Track
	level      string
	canceled   []uint64
	toasts     []string
	managers   int
}

func (h *downloadTestHost) DesktopSettings() pluginapi.DesktopSettings { return h.desktop }
func (h *downloadTestHost) Playback() pluginapi.PlaybackControls       { return h.playback }
func (h *downloadTestHost) Artwork(string, int) (*ui.Bitmap, bool)     { return nil, false }
func (h *downloadTestHost) Player() PlayerPort                         { return nil }
func (h *downloadTestHost) Invalidate()                                {}
func (h *downloadTestHost) Toast(message string)                       { h.toasts = append(h.toasts, message) }

func (h *downloadTestHost) RequestDownloadOptions(tr pluginapi.Track, token string) {
	h.requested = append(h.requested, tr)
	h.asked = append(h.asked, token)
	h.desktop.DownloadOptions, h.desktop.DownloadOptionsToken = h.probe(tr), token
}

func (h *downloadTestHost) Download(tracks []pluginapi.Track, level string) {
	h.downloaded, h.level = tracks, level
}

func (h *downloadTestHost) CancelDownload(id uint64) { h.canceled = append(h.canceled, id) }

func (h *downloadTestHost) OpenDownloads() { h.managers++ }

func (h *downloadTestHost) ApplyDesktopPreferences(preferences pluginapi.DesktopPreferences) {
	h.desktop.Preferences = preferences
}

func (h *downloadTestHost) TakeToast() string                      { return "" }
func (h *downloadTestHost) CloseSettings()                         {}
func (h *downloadTestHost) DecoderStatus() pluginapi.DecoderStatus { return pluginapi.DecoderStatus{} }

var downloadTestTrack = pluginapi.Track{
	ID: 7, Ref: "0039MnYb0qxYhV", Name: "宝船",
	Artists: []pluginapi.Artist{{Name: "DOUDOU"}},
}

// downloadTestWindow draws a song's row with its menu, as a page does,
// with the download dialog over it.
func downloadTestWindow(home *Home, tracks ...pluginapi.Track) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		home.renderTracks(c, staticTracks(tracks), TrackView{Menu: home.trackMenu})
		home.downloadDialog(c)
	}, 1200, 800)
}

// The dialog opens on the quality remembered, else the platform's, else
// the one closest to them.
func TestDownloadLevelDefaults(t *testing.T) {
	choices := downloadChoices([][]pluginapi.DownloadOption{{
		{Level: "standard", Format: "mp3", Available: true},
		{Level: "exhigh", Format: "mp3", Available: true},
		{Level: "lossless", Format: "flac", Available: false, Reason: "需要登录或开通会员"},
		{Level: "hires", Available: false, Reason: "该曲目没有这个音质"},
	}})
	cases := []struct {
		name               string
		preferred, quality string
		want               string
	}{
		{"the quality remembered", "standard", "lossless", "standard"},
		{"the platform's quality", "", "exhigh", "exhigh"},
		{"closest to the platform's quality", "", "hires", "exhigh"},
		{"the cheapest one when nothing is asked for", "", "", "standard"},
		{"the one at or above on a tie", "", "higher", "exhigh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defaultDownloadLevel(c.preferred, c.quality, choices); got != c.want {
				t.Fatalf("chose %q, want %q", got, c.want)
			}
		})
	}
	if got := defaultDownloadLevel("standard", "standard", nil); got != "" {
		t.Fatalf("chose %q with nothing on offer", got)
	}
}

// One batch's qualities say what each of them serves, and how many songs
// of the batch they serve it for.
func TestDownloadChoicesAggregateTracks(t *testing.T) {
	one := []pluginapi.DownloadOption{
		{Level: "standard", Format: "mp3", Size: 3 << 20, Available: true},
		{Level: "exhigh", Format: "mp3", Size: 10 << 20, Available: true},
		{Level: "lossless", Format: "flac", Available: false, Reason: "需要登录或开通会员"},
	}
	two := []pluginapi.DownloadOption{
		{Level: "standard", Format: "mp3", Size: 4 << 20, Available: true},
		{Level: "higher", Format: "mp3", Size: 10 << 20, Available: true},
		{Level: "lossless", Format: "flac", Available: false, Reason: "需要登录或开通会员"},
	}
	choices := downloadChoices([][]pluginapi.DownloadOption{one, two})
	if len(choices) != 3 {
		t.Fatalf("a batch of two answered with %d choices: %+v", len(choices), choices)
	}
	// QQ Music serves higher and exhigh alike: the two are one choice, with
	// both songs counted for what it serves.
	if choices[0].level != "standard" || !choices[0].available() ||
		choices[0].served != 2 || choices[0].total != 2 || choices[0].format != "mp3" {
		t.Fatalf("standard read %+v", choices[0])
	}
	if choices[1].level != "exhigh" || !choices[1].available() ||
		choices[1].served != 2 || choices[1].total != 2 || choices[1].format != "mp3" {
		t.Fatalf("320k read %+v", choices[1])
	}
	if choices[2].available() || choices[2].total != 2 || choices[2].reason != "需要登录或开通会员" {
		t.Fatalf("a quality no song serves reads %+v", choices[2])
	}
	// One song's qualities are counted for that one song, the size of which
	// it would take.
	single := downloadChoices([][]pluginapi.DownloadOption{one})[1]
	if !single.available() || single.total != 1 || single.size != 10<<20 {
		t.Fatalf("one song's 320k reads %+v", single)
	}
	// A song whose probe answered nothing says nothing about the batch.
	silent := downloadChoices([][]pluginapi.DownloadOption{one, nil})
	if len(silent) != 3 || silent[1].total != 1 || silent[1].served != 1 {
		t.Fatalf("a batch of one answering reads %+v", silent)
	}
}

// A directory downloads can be written to is made when it is missing, and
// a file where one should be is not a directory.
func TestDownloadDirWritable(t *testing.T) {
	dir := t.TempDir()
	if err := downloadDirWritable(dir); err != nil {
		t.Fatalf("a fresh directory is not writable: %v", err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("the check left %d files behind", len(left))
	}
	nested := filepath.Join(dir, "songs", "2026")
	if err := downloadDirWritable(nested); err != nil {
		t.Fatalf("a missing directory is not made: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || !info.IsDir() {
		t.Fatalf("the check did not make the directory: %v", err)
	}
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadDirWritable(file); err == nil {
		t.Fatal("a file was taken for a directory to download into")
	}
	if err := downloadDirWritable(""); err == nil {
		t.Fatal("an empty directory was taken for a writable one")
	}
}

// Only a host that downloads songs offers the entry, and a song already
// coming in says so and stays out of reach. The host names a download for
// the file it writes, cleaning what the file system refuses, so the song
// already coming in is the one whose own reference matches.
func TestDownloadEntryNeedsTheHost(t *testing.T) {
	hard := pluginapi.Track{
		ID: 8, Ref: "0039MnYb0qxYhW", Name: "南三环/夜",
		Artists: []pluginapi.Artist{{Name: "DOUDOU"}},
	}
	host := &downloadTestHost{}
	home := NewHome(NewState(), host)
	tt := downloadTestWindow(home, downloadTestTrack, hard)
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(tt.Menu(), "下载") {
		t.Fatalf("a host that does not download songs offered one: %v", tt.Menu())
	}
	tt.CloseMenu()

	host.desktop.DownloadAvailable = true
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "下载") {
		t.Fatalf("a host that downloads songs offered nothing: %v", tt.Menu())
	}
	tt.CloseMenu()

	host.desktop.Downloads = []pluginapi.DownloadItem{{
		ID: 1, Batch: 1, Name: "DOUDOU - 南三环_夜", Ref: hard.Ref,
		Level: "lossless", Phase: protocol.TaskRunning,
	}}
	tt.Frame()
	if err := tt.RightClick("南三环/夜"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), "下载中…") {
		t.Fatalf("a song whose file name was cleaned is offered again: %v", tt.Menu())
	}
	err := tt.ChooseMenuItem("下载中…")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("a song already coming in could be downloaded again: %v", err)
	}
}

// Picking 下载 asks the host what the song may be downloaded as, and the
// dialog starts what the listener chose.
func TestDownloadDialogStartsTheChosenLevel(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	host.probe = func(pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{
			{Level: "standard", Format: "mp3", Size: 4 << 20, Available: true},
			{Level: "exhigh", Format: "mp3", Size: 10 << 20, Available: true},
			{Level: "lossless", Format: "flac", Size: 32 << 20, Available: true},
			{Level: "hires", Available: false, Reason: "需要登录或开通会员"},
		}
	}
	home := NewHome(NewState(), host)
	home.preferences = &platformPreferences{Quality: "lossless"}
	saves := 0
	home.savePreferences = func() { saves++ }
	tt := downloadTestWindow(home, downloadTestTrack)
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下载"); err != nil {
		t.Fatal(err)
	}
	if len(host.requested) != 1 || host.requested[0].ID != downloadTestTrack.ID {
		t.Fatalf("the probe asked about %+v", host.requested)
	}
	if host.asked[0] == "" {
		t.Fatal("the probe went out without a token to answer it by")
	}
	tt.Frame()
	if !tt.HasText("下载「宝船」") {
		t.Fatal("the dialog did not open with the probed qualities")
	}
	if home.download.level != "lossless" {
		t.Fatalf("the dialog opened on %q, not on the platform's quality", home.download.level)
	}
	if !tt.HasText("32 MB") || !tt.HasText("需要登录或开通会员") {
		t.Fatalf("the dialog does not say what the song may be downloaded as: %v", tt.Texts())
	}
	_ = tt.Click("Hi-Res（FLAC）")
	if home.download.level != "lossless" {
		t.Fatalf("a quality the platform does not serve was chosen: %q", home.download.level)
	}
	if err := tt.Click("标准（128k）"); err != nil {
		t.Fatal(err)
	}
	if home.download.level != "standard" || saves != 1 || home.preferences.DownloadLevel != "standard" {
		t.Fatalf("choosing a quality left level %q, %d saves", home.download.level, saves)
	}
	tt.Frame()
	if err := tt.Click("开始下载"); err != nil {
		t.Fatal(err)
	}
	if len(host.downloaded) != 1 || host.downloaded[0].ID != downloadTestTrack.ID || host.level != "standard" {
		t.Fatalf("the download went out as %+v at %q", host.downloaded, host.level)
	}
	tt.Frame()
	if tt.HasText("开始下载") {
		t.Fatal("the dialog stayed open after starting the download")
	}
}

// A directory nothing may be written to is said before the download
// starts, and keeps 开始下载 out of reach.
func TestDownloadDialogRefusesAnUnwritableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: file,
	}}
	host.probe = func(pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{{Level: "standard", Format: "mp3", Available: true}}
	}
	home := NewHome(NewState(), host)
	tt := downloadTestWindow(home, downloadTestTrack)
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("下载目录不可用") {
		t.Fatalf("the dialog does not say why: %v", tt.Texts())
	}
	start, ok := tt.Access("开始下载")
	if !ok || !start.Disabled {
		t.Fatalf("开始下载 is not out of reach with an unwritable directory: %+v", start)
	}
	_ = tt.Click("开始下载")
	if len(host.downloaded) != 0 {
		t.Fatalf("the host was asked to download into a directory nothing can be written to: %+v", host.downloaded)
	}
}

// A host that never settled on a directory is said when the dialog opens,
// not only when the directory changes under it, and keeps 开始下载 out of
// reach: the first frame checks where the songs would go too.
func TestDownloadDialogChecksTheDirectoryOnTheFirstFrame(t *testing.T) {
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{DownloadAvailable: true}}
	host.probe = func(pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{{Level: "standard", Format: "mp3", Available: true}}
	}
	home := NewHome(NewState(), host)
	tt := downloadTestWindow(home)
	home.beginDownload([]pluginapi.Track{downloadTestTrack})
	tt.Frame()
	if !home.download.open || home.download.dirErr == "" {
		t.Fatalf("a host without a directory opened %+v", home.download)
	}
	start, ok := tt.Access("开始下载")
	if !ok || !start.Disabled {
		t.Fatalf("开始下载 is live with nowhere to download to: %+v", start)
	}
	_ = tt.Click("开始下载")
	if len(host.downloaded) != 0 {
		t.Fatalf("the host was asked to download with no directory: %+v", host.downloaded)
	}
}

// A batch whose probe answered nothing opens the dialog with nothing on
// offer, and keeps 开始下载 out of reach: there is no quality to start it at.
func TestDownloadDialogWithNoQuality(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	host.probe = func(pluginapi.Track) []pluginapi.DownloadOption { return nil }
	home := NewHome(NewState(), host)
	tt := downloadTestWindow(home)
	home.beginDownload([]pluginapi.Track{downloadTestTrack})
	tt.Frame()
	if !home.download.open {
		t.Fatal("a batch no song answered for did not open the dialog")
	}
	if len(home.download.choices) != 0 || home.download.level != "" {
		t.Fatalf("the dialog offered %+v at %q", home.download.choices, home.download.level)
	}
	start, ok := tt.Access("开始下载")
	if !ok || !start.Disabled {
		t.Fatalf("开始下载 is live with no quality to download at: %+v", start)
	}
	_ = tt.Click("开始下载")
	if len(host.downloaded) != 0 {
		t.Fatalf("the host was asked to download with no quality: %+v", host.downloaded)
	}
}

// An answer is only taken for the ask it came with: one carrying another
// token says nothing about this batch, and the ask stays open for its own
// answer rather than being lost.
func TestDownloadIgnoresAnAnswerForAnotherAsk(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	probe := func(pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{{Level: "standard", Format: "mp3", Available: true}}
	}
	host.probe = probe
	home := NewHome(NewState(), host)
	tt := downloadTestWindow(home)
	home.beginDownload([]pluginapi.Track{downloadTestTrack})
	if len(host.asked) != 1 {
		t.Fatalf("the batch asked %d times", len(host.asked))
	}
	host.desktop.DownloadOptions = []pluginapi.DownloadOption{{Level: "hires", Available: true}}
	host.desktop.DownloadOptionsToken = "another-ask"
	tt.Frame()
	if home.download.open || home.downloadPending == nil {
		t.Fatalf("an answer for another ask settled the batch: open %v, pending %v",
			home.download.open, home.downloadPending)
	}
	host.desktop.DownloadOptions, host.desktop.DownloadOptionsToken = probe(downloadTestTrack), host.asked[0]
	tt.Frame()
	if !home.download.open || home.download.level != "standard" {
		t.Fatalf("this batch's own answer opened %+v", home.download)
	}
}

// A second ask while one is in flight is not sent: the host keeps the
// answer it gave last, so asking again would bury the first answer and
// leave that batch waiting for an answer that never comes.
func TestDownloadTakesOneAskAtATime(t *testing.T) {
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: t.TempDir(),
	}}
	host.probe = func(pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{{Level: "standard", Format: "mp3", Available: true}}
	}
	home := NewHome(NewState(), host)
	home.beginDownload([]pluginapi.Track{downloadTestTrack})
	home.beginDownload([]pluginapi.Track{{ID: 8, Ref: "0039MnYb0qxYhW", Name: "南三环"}})
	if len(host.asked) != 1 || len(host.requested) != 1 {
		t.Fatalf("a second ask went out while one was in flight: asked %v", host.asked)
	}
	if home.downloadPending == nil || home.downloadPending.tracks[0].ID != downloadTestTrack.ID {
		t.Fatalf("the batch in flight became %+v", home.downloadPending)
	}
}

// The settings window shows the download group only for a host that
// downloads songs, cancelling from it and clearing the directory.
func TestDownloadSettingsFollowTheHost(t *testing.T) {
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true,
		DownloadDir:       "/tmp/songs",
		Preferences:       pluginapi.DesktopPreferences{DownloadDir: "/tmp/songs"},
		Downloads: []pluginapi.DownloadItem{
			{ID: 1, Batch: 1, Name: "DOUDOU - 宝船", Phase: protocol.TaskRunning, Done: 4 << 20, Total: 32 << 20},
			{ID: 2, Batch: 1, Name: "DOUDOU - 南三环", Phase: protocol.TaskSucceeded, Path: "/tmp/songs/DOUDOU - 南三环.flac"},
		},
	}}
	home := NewHome(NewState(), host)
	home.preferences = &platformPreferences{Quality: "standard"}
	tt := ui.NewTester(home.settingsWindow, 560, 1200)
	tt.Frame()
	if !tt.HasText("12%") || !tt.HasText("4 MB") || !tt.HasText("32 MB") {
		t.Fatalf("the song coming in shows no progress: %v", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if len(host.canceled) != 1 || host.canceled[0] != 1 {
		t.Fatalf("取消 canceled %v", host.canceled)
	}
	if err := tt.Click("打开下载管理器"); err != nil {
		t.Fatal(err)
	}
	if host.managers != 1 {
		t.Fatalf("打开 opened the download manager %d times, want once", host.managers)
	}
	if err := tt.Click("恢复默认"); err != nil {
		t.Fatal(err)
	}
	if got := host.desktop.Preferences.DownloadDir; got != "" {
		t.Fatalf("恢复默认 left the directory at %q", got)
	}
	// A host that does not download songs has no group at all.
	host.desktop = pluginapi.DesktopSettings{Preferences: pluginapi.DesktopPreferences{DownloadDir: "/tmp/songs"}}
	tt.Frame()
	if tt.HasText("下载目录") {
		t.Fatalf("a host that does not download songs showed the group: %v", tt.Texts())
	}
}

// A batch's availability says how much of the batch each quality serves:
// all of it, some of it, or none.
func TestDownloadBatchAvailability(t *testing.T) {
	lossless := func(available bool) []pluginapi.DownloadOption {
		option := pluginapi.DownloadOption{Level: "lossless", Format: "flac", Available: true}
		if !available {
			option.Available, option.Reason = false, "需要登录或开通会员"
		}
		return []pluginapi.DownloadOption{option}
	}
	batch := func(served, of int) downloadChoice {
		answers := make([][]pluginapi.DownloadOption, of)
		for i := range answers {
			answers[i] = lossless(i < served)
		}
		return downloadChoices(answers)[0]
	}
	cases := []struct {
		name      string
		served    int
		of        int
		available bool
	}{
		{"a quality every song serves", 5, 5, true},
		{"a quality some songs serve", 2, 5, true},
		{"a quality no song serves", 0, 5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			choice := batch(c.served, c.of)
			if choice.available() != c.available || choice.served != c.served || choice.total != c.of {
				t.Fatalf("%+v; want %d of %d available, %v", choice, c.served, c.of, c.available)
			}
		})
	}
}

// A list longer than the host queues is refused here, with the ceiling said,
// and a list exactly at the ceiling still goes.
func TestDownloadRefusesTooManyTracks(t *testing.T) {
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{DownloadAvailable: true}}
	host.probe = func(pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{{Level: "standard", Format: "mp3", Available: true}}
	}
	home := NewHome(NewState(), host)
	home.beginDownload(make([]pluginapi.Track, downloadMaxTracks+1))
	if len(host.requested) != 0 || home.downloadPending != nil {
		t.Fatalf("a list of %d songs went to the host", downloadMaxTracks+1)
	}
	if len(host.toasts) != 1 || !strings.Contains(host.toasts[0], strconv.Itoa(downloadMaxTracks)) {
		t.Fatalf("the refusal said %q", host.toasts)
	}
	home.beginDownload(make([]pluginapi.Track, downloadMaxTracks))
	if home.downloadPending == nil || len(home.downloadPending.tracks) != downloadMaxTracks {
		t.Fatal("a list at the ceiling was not taken")
	}
	if len(host.requested) != 1 {
		t.Fatalf("a list at the ceiling probed %d songs", len(host.requested))
	}
}

// A page's entry takes its whole list into the dialog a song does — one
// quality, the availability of the whole list, one batch — before the host
// is asked to download it.
func TestPageDownloadTakesTheWholeList(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	host.probe = func(tr pluginapi.Track) []pluginapi.DownloadOption {
		return []pluginapi.DownloadOption{
			{Level: "exhigh", Format: "mp3", Size: 10 << 20, Available: true},
			{Level: "lossless", Format: "flac", Available: tr.ID != 1, Reason: "需要登录或开通会员"},
		}
	}
	home := NewHome(NewState(), host)
	home.preferences = &platformPreferences{Quality: "lossless"}
	songs := []pluginapi.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}, {ID: 3, Name: "三"}}
	home.route = Route{Page: PagePlaylist, ID: 5}
	home.playlists[5] = &playlistPage{pl: pluginapi.Playlist{ID: 5, Name: "歌单", Tracks: songs}}
	tt := ui.NewTester(func(c *ui.Context) {
		home.playlistPage(c)
		home.downloadDialog(c)
	}, 1200, 800)
	if err := tt.Click("下载全部"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !home.download.open {
		t.Fatal("the page's entry did not open the dialog")
	}
	if len(host.requested) != len(songs) {
		t.Fatalf("the page probed %d of its %d songs", len(host.requested), len(songs))
	}
	for i, tr := range host.requested {
		if tr.ID != songs[i].ID {
			t.Fatalf("the page probed %+v, not its own list", host.requested)
		}
	}
	if tt.HasText("下载「一」") {
		t.Fatal("the batch dialog opened as one song's")
	}
	var lossless downloadChoice
	for _, choice := range home.download.choices {
		if choice.level == "lossless" {
			lossless = choice
		}
	}
	if lossless.served != 2 || lossless.total != 3 {
		t.Fatalf("the dialog says the list serves %+v", lossless)
	}
	if err := tt.Click("开始下载"); err != nil {
		t.Fatal(err)
	}
	if len(host.downloaded) != len(songs) || host.level != "lossless" {
		t.Fatalf("the batch went out as %+v at %q", host.downloaded, host.level)
	}
	for i, tr := range host.downloaded {
		if tr.ID != songs[i].ID {
			t.Fatalf("the host was given %+v, not the page's list", host.downloaded)
		}
	}
}

// Every page with a whole list offers it to a host that downloads songs, and
// none of them does without one; while a batch is coming in they say so and
// stay out of the host's queue.
func TestPageDownloadEntriesFollowTheHost(t *testing.T) {
	songs := []pluginapi.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}}
	playing := &plugintest.Player{}
	playing.PlayQueue(songs, 0)
	pages := []struct {
		name   string
		label  string
		setup  func(home *Home)
		render func(home *Home, c *ui.Context)
	}{
		{
			name: "the playlist page", label: "下载全部",
			setup: func(home *Home) {
				home.route = Route{Page: PagePlaylist, ID: 5}
				home.playlists[5] = &playlistPage{pl: pluginapi.Playlist{ID: 5, Name: "歌单", Tracks: songs}}
			},
			render: func(home *Home, c *ui.Context) { home.playlistPage(c) },
		},
		{
			name: "the album page", label: "下载全部",
			setup: func(home *Home) {
				home.route = Route{Page: PageAlbum, ID: 6}
				home.albums[6] = &albumPage{
					al:     backend.AlbumProfile{Album: backend.Album{ID: 6, Name: "专辑"}},
					tracks: songs,
				}
			},
			render: func(home *Home, c *ui.Context) { home.albumPage(c) },
		},
		{
			name: "the queue", label: "下载队列",
			setup:  func(home *Home) { home.npPane = paneQueue },
			render: func(home *Home, c *ui.Context) { home.queuePane(c) },
		},
	}
	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			host := &downloadTestHost{playback: playing}
			home := NewHome(NewState(), host)
			page.setup(home)
			tt := ui.NewTester(func(c *ui.Context) { page.render(home, c) }, 1200, 800)
			if tt.HasText(page.label) {
				t.Fatalf("a host that does not download songs offered %q: %v", page.label, tt.Texts())
			}
			host.desktop.DownloadAvailable = true
			tt.Frame()
			if !tt.HasText(page.label) {
				t.Fatalf("a host that downloads songs offered no %q: %v", page.label, tt.Texts())
			}
			// One batch at a time: the entry says so and takes no click.
			host.desktop.Downloads = []pluginapi.DownloadItem{
				{ID: 1, Batch: 1, Name: "DOUDOU - 宝船", Phase: protocol.TaskRunning},
			}
			tt.Frame()
			entry, ok := tt.Access("下载中…")
			if !ok || !entry.Disabled {
				t.Fatalf("a batch coming in left the entry live: %+v", entry)
			}
			// A disabled pill takes no press: the entry is there to press,
			// and nothing goes to the host.
			if err := tt.Click("下载中…"); err != nil {
				t.Fatalf("the entry could not be pressed at all: %v", err)
			}
			tt.Frame()
			if len(host.requested) != 0 {
				t.Fatalf("the page queued itself again while a batch was coming in: %+v", host.requested)
			}
			// A probe still in flight says so too: the list is not probed
			// again under it, as the host answers only the ask it took last.
			host.desktop.Downloads = nil
			home.downloadPending = &pendingDownload{tracks: songs}
			tt.Frame()
			checking, ok := tt.Access("正在检查音质…")
			if !ok || !checking.Disabled {
				t.Fatalf("a probe in flight left the entry live: %+v", checking)
			}
			if err := tt.Click("正在检查音质…"); err != nil {
				t.Fatalf("the entry could not be pressed at all: %v", err)
			}
			if len(host.requested) != 0 {
				t.Fatalf("the page queued itself again while a probe was in flight: %+v", host.requested)
			}
		})
	}
}
