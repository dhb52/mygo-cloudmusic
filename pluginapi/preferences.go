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
}

type DesktopSettings struct {
	Preferences     DesktopPreferences
	CacheUsed       int64
	NotchAvailable  bool
	Updates         bool
	UpdateChecks    bool
	UpdateDownloads bool
	LastUpdateCheck time.Time
	CacheTask       DesktopTask
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
