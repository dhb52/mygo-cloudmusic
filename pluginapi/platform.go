package pluginapi

import (
	"context"

	"github.com/egoist/mygo/ui"
)

// Adapter describes the status and media that the host routes between plugins and services.
type Adapter interface {
	Configured() bool
	// FetchArtwork reports whether this plugin recognized the resource reference.
	FetchArtwork(ctx context.Context, ref string, size int) (data []byte, handled bool, err error)
	PlaybackMedia() PlaybackMedia
	OwnsTrack(*Track) bool
}

// Registration supplies the entry point for a platform instance.
type Registration struct {
	Info
	Start func(Startup) Adapter
	// LegacySettingsKey keeps a former top-level namespace in the same place.
	LegacySettingsKey string
	// LegacySettingsFields keeps selected fields at the root of the old settings file.
	LegacySettingsFields []string
}

// Info is a platform's identity presented by the host.
type Info struct {
	ID   ID
	Name string
	// Icon is supplied by the plugin for display by the host.
	Icon *ui.SVG
}

type PlayerPort interface {
	Current() *Track
	Playing() bool
	Loading() bool
	Err() string
	TogglePlay()
	Next()
	Queue() []Track
	PlayOrder() ([]int, int)
	PlayQueue([]Track, int)
	AppendQueue([]Track)
	PlayList([]Track, int, QueueFeed)
	PlayRadio(string, []Track, QueueFeed)
	EndRadio()
	Radio() string
	SetShuffle(bool)
}

// Services provides desktop and media operations to window-owning plugins.
type Services interface {
	DesktopServices
	DesktopPlaybackServices
	Post(func())
	Invalidate()
	CurrentPlatform() ID
	ShowMain()
	OpenSettings()
	CloseSettings()
	// QuitApp asks the host to quit the application, which is what the
	// listener meant by quitting from a plugin's own window, menu or tray.
	QuitApp()
	// OpenDownloads shows the host's download manager: what is being
	// fetched, what waits, and what the listener has already kept.
	OpenDownloads()
	Toast(string)
	TakeToast() string
	Player() PlayerPort
	Artwork(url string, size int) (*ui.Bitmap, bool)
	PlaybackLevels() ([SpectrumBands]float32, bool)
	CopyText(string)
	GlobalSearchHistory() []string
	RememberGlobalSearch(string)
	ClearSearchHistory()
}
