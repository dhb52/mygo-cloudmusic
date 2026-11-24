package pluginapi

import (
	"time"

	"github.com/waygu-music-plugin/sdk/protocol"
)

type DesktopTask = protocol.DesktopTask

// DesktopPreferences are application-wide settings used by the active plugin.
type DesktopPreferences struct {
	Appearance    string
	AppIcon       string
	CacheMB       int
	Notch         bool
	NotchActivity bool
	NotchVirtual  bool
	Smoke         bool
	Fog           bool

	UseMPV         bool
	MPVExclusive   bool
	MPVGapless     bool
	MPVAudioDevice string

	// DownloadDir is where downloads are written. Empty means the system
	// download directory.
	DownloadDir string
}

type DesktopSettings struct {
	DesktopLyricsColorAvailable bool
	DesktopLyricsColor          string
	StatusLyricsAvailable       bool
	StatusLyricsEnabled         bool
	TaskbarLyricsFontSize       int // Zero when the system bar uses its own font size.
	TaskbarLyricsFontFamily     string
	LyricsFontAvailable         bool
	LyricsFontFamily            string
	Preferences                 DesktopPreferences
	CacheUsed                   int64
	NotchAvailable              bool
	Updates                     bool
	UpdateChecks                bool
	UpdateDownloads             bool
	LastUpdateCheck             time.Time
	CacheTask                   DesktopTask

	// DownloadAvailable advertises a host that downloads songs. DownloadDir is
	// the effective directory; Downloads is the latest download state pushed
	// for the settings window.
	DownloadAvailable bool
	DownloadDir       string
	Downloads         []DownloadItem
}

type AudioDevice struct {
	Name        string
	Description string
}

type DecoderStatus struct {
	Ready                    bool
	Downloaded               bool
	Downloading              bool
	SystemPath               string
	Done, Total              int64
	Devices                  []AudioDevice
	DownloadTask, DeleteTask DesktopTask
}

func (s DecoderStatus) LatestTask() DesktopTask {
	if s.DeleteTask.ID > s.DownloadTask.ID {
		return s.DeleteTask
	}
	return s.DownloadTask
}

// DesktopServices reads and applies settings without drawing any controls.
type DesktopServices interface {
	DesktopSettings() DesktopSettings
	ApplyDesktopPreferences(DesktopPreferences)
	ClearAudioCache()
	DecoderStatus() DecoderStatus
	StartDecoderDownload()
	CancelDecoderDownload()
	DeleteDecoder()
	SetUpdateChecks(bool)
	SetUpdateDownloads(bool)
	CheckForUpdates()

	// The download calls queue and return. Download starts a batch one track at
	// a time at the asked level; the platform may serve lower, and the level
	// the file really is comes back in DownloadItem.Level.
	Download(tracks []Track, level string)
	CancelDownload(id uint64)
}

// LyricsSettingsServices is optional. The availability fields in DesktopSettings
// advertise which controls the current host supports.
type LyricsSettingsServices interface {
	SetDesktopLyricsColor(string)
	SetStatusLyrics(bool)
}

const (
	TaskbarLyricsMinFontSize     = 12
	TaskbarLyricsMaxFontSize     = 20
	TaskbarLyricsDefaultFontSize = 15
)

// TaskbarLyricsFontServices is optional for hosts with a resizable lyric font.
type TaskbarLyricsFontServices interface {
	SetTaskbarLyricsFontSize(int)
	SetTaskbarLyricsFontFamily(string)
}

// LyricsFontServices is optional for hosts with a shared lyric font setting.
type LyricsFontServices interface {
	SetLyricsFontFamily(string)
}

// DesktopLyricsPreferences are the desktop lyric window's persistent choices.
// A pointer in settings keeps older settings files compatible: missing
// desktop-lyrics data gets the current defaults on first use.
type DesktopLyricsPreferences struct {
	Locked          bool    `json:"locked,omitempty"`
	ShowTranslation bool    `json:"showTranslation"`
	FontSize        float32 `json:"fontSize,omitempty"`
	Color           int     `json:"color,omitempty"`
	X               int     `json:"x,omitempty"`
	Y               int     `json:"y,omitempty"`
	PositionSet     bool    `json:"positionSet,omitempty"`
}

func DefaultDesktopLyricsPreferences() DesktopLyricsPreferences {
	return DesktopLyricsPreferences{ShowTranslation: true, FontSize: 30}
}
