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

// downloadTestHost answers the download calls as a host does.
type downloadTestHost struct {
	Host
	desktop    pluginapi.DesktopSettings
	playback   pluginapi.PlaybackControls
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

// The dialog opens on the quality remembered when it offers it, else the
// platform's own, else the level closest to it.
func TestDownloadLevelDefaults(t *testing.T) {
	cases := []struct {
		name               string
		preferred, quality string
		want               string
	}{
		{"the quality remembered", "standard", "lossless", "standard"},
		{"the platform's quality", "", "exhigh", "higher"},
		{"closest to the platform's quality", "", "hires", "lossless"},
		{"the cheapest one when nothing is asked for", "", "", "standard"},
		{"the platform's when the remembered one is not offered", "hires", "standard", "standard"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defaultDownloadLevel(c.preferred, c.quality); got != c.want {
				t.Fatalf("chose %q, want %q", got, c.want)
			}
		})
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

// Picking 下载 opens the quality dialog there and then, on the plugin's own
// levels: the host is asked nothing before the listener starts the download.
func TestDownloadDialogOpensAtOnce(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	home := NewHome(NewState(), host)
	home.preferences = &platformPreferences{Quality: "lossless"}
	tt := downloadTestWindow(home, downloadTestTrack)
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !home.download.open || !tt.HasText("下载「宝船」") {
		t.Fatalf("the dialog did not open on the song at once: %+v", home.download)
	}
	if home.download.level != "lossless" {
		t.Fatalf("the dialog opened on %q, not the platform's quality", home.download.level)
	}
	if len(host.downloaded) != 0 {
		t.Fatalf("the host was asked to download before the listener started it: %+v", host.downloaded)
	}
}

// Picking a quality in the dialog remembers it, and 开始下载 asks the host
// to download the batch at it.
func TestDownloadDialogStartsTheChosenLevel(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	home := NewHome(NewState(), host)
	home.preferences = &platformPreferences{Quality: "standard"}
	saves := 0
	home.savePreferences = func() { saves++ }
	tt := downloadTestWindow(home, downloadTestTrack)
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("无损（FLAC）"); err != nil {
		t.Fatal(err)
	}
	if home.download.level != "lossless" || saves != 1 || home.preferences.DownloadLevel != "lossless" {
		t.Fatalf("choosing a quality left level %q, %d saves", home.download.level, saves)
	}
	tt.Frame()
	if err := tt.Click("开始下载"); err != nil {
		t.Fatal(err)
	}
	if len(host.downloaded) != 1 || host.downloaded[0].ID != downloadTestTrack.ID || host.level != "lossless" {
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

// A list longer than the host queues is refused here, with the ceiling said,
// and a list exactly at the ceiling still opens the dialog.
func TestDownloadRefusesTooManyTracks(t *testing.T) {
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{DownloadAvailable: true}}
	home := NewHome(NewState(), host)
	home.beginDownload(make([]pluginapi.Track, downloadMaxTracks+1))
	if home.download.open {
		t.Fatal("a list over the ceiling opened the dialog")
	}
	if len(host.toasts) != 1 || !strings.Contains(host.toasts[0], strconv.Itoa(downloadMaxTracks)) {
		t.Fatalf("the refusal said %q", host.toasts)
	}
	home.beginDownload(make([]pluginapi.Track, downloadMaxTracks))
	if !home.download.open || len(home.download.tracks) != downloadMaxTracks {
		t.Fatal("a list at the ceiling was not taken")
	}
}

// A page's entry takes its whole list into the dialog, and 开始下载 hands
// the whole batch to the host at the quality chosen.
func TestPageDownloadTakesTheWholeList(t *testing.T) {
	dir := t.TempDir()
	host := &downloadTestHost{desktop: pluginapi.DesktopSettings{
		DownloadAvailable: true, DownloadDir: dir,
		Preferences: pluginapi.DesktopPreferences{DownloadDir: dir},
	}}
	home := NewHome(NewState(), host)
	home.preferences = &platformPreferences{Quality: "standard"}
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
	if len(home.download.tracks) != len(songs) {
		t.Fatalf("the dialog holds %d of the page's %d songs", len(home.download.tracks), len(songs))
	}
	if tt.HasText("下载「一」") {
		t.Fatal("the batch dialog opened as one song's")
	}
	if err := tt.Click("无损（FLAC）"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
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
			if len(host.downloaded) != 0 {
				t.Fatalf("the page started a download while a batch was coming in: %+v", host.downloaded)
			}
		})
	}
}
