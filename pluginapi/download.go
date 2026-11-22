package pluginapi

import "github.com/waygu-music-plugin/sdk/protocol"

// DownloadOption is one quality a track may be downloaded at.
type DownloadOption struct {
	Level     string // standard/higher/exhigh/lossless/hires
	Format    string // mp3 or flac, as probed
	Size      int64  // bytes; zero when unknown
	Available bool
	Reason    string // why it is unavailable, shown to the listener
}

// DownloadItem is one track's download in the settings window.
type DownloadItem struct {
	ID    uint64
	Batch uint64
	Name  string // artist - song
	// Ref is the song's own reference on its platform, which the plugin
	// asks with and matches on: the name is the listener's to read and
	// may be cleaned up on the way to a file system.
	Ref   string
	Level string
	Phase protocol.TaskPhase
	Done  int64
	Total int64 // zero when unknown
	Path  string
	// Message is why the download failed, or what was found instead of
	// fetching, as when the song was there already.
	Message string
}
