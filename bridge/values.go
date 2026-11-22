package bridge

import (
	"encoding/json"
	"time"

	"github.com/waygu-music-plugin/sdk/platformrpc"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/eq"
	"github.com/waygu-music-plugin/sdk/pluginapi/lyrics"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// The desktop bridge converts its existing in-process value types at the
// boundary. Only values are sent through RPC, never windows or callbacks.
type PluginPrepare struct {
	Scope      platformrpc.Scope
	Facade     PluginSnapshot
	Settings   json.RawMessage
	Tray       bool
	Appearance string
	WindowIcon []byte
}

type PluginAction = protocol.PluginAction
type PluginActionReply = protocol.PluginActionReply

type PluginCall struct {
	Scope        platformrpc.Scope
	Action       string
	Track        pluginapi.Track
	Level        string
	Format       string
	Ref          string
	Size         int32
	DeadlineNano int64
	MediaStream  uint32
	PreviousURL  string
	FeedCursor   *pluginapi.QueueFeedCursor
}

type PluginMedia struct {
	Failure   platformrpc.Failure
	Stream    *PluginSource
	Original  string
	Trans     string
	Word      string
	Cover     []byte
	CoverSize uint32
	Handled   bool
	Tracks    []pluginapi.Track
}

type PluginSource struct {
	URL, Type, Level string
	Size             int64
	Gain, Peak       float64
	Headers          map[string][]string
	Renewable        bool
	Digest           *pluginapi.ContentDigest
}

type PluginSnapshot struct {
	Current            *pluginapi.Track
	Queue              []pluginapi.Track
	Order              []int
	QueueRevision      uint64
	Attempt            uint64
	UnchangedQueue     bool
	At                 int
	Playing, Loading   bool
	Ready, Shuffle     bool
	Repeat             pluginapi.RepeatMode
	Radio, Error       string
	Position, Duration time.Duration
	Downloaded         float64
	DownloadKnown      bool
	Lyrics             []lyrics.Line
	LyricsLoaded       bool
	Levels             [pluginapi.SpectrumBands]float32
	LevelsLive         bool
	Stream             *PluginSource
	Volume             float64
	Presentation       pluginapi.Presentation
	Desktop            pluginapi.DesktopSettings
	Decoder            pluginapi.DecoderStatus
	Casting            pluginapi.CastingState
	EQ                 eq.Settings
	EQAvailable        bool
	Search             []string
}

type PluginHostCall struct {
	Scope         platformrpc.Scope
	Action        string
	Tracks        []pluginapi.Track
	Index         int
	Position      time.Duration
	Number        float64
	Flag          bool
	Text          string
	Level         string
	Feed          string
	Session       *pluginapi.PlaybackSession
	Settings      json.RawMessage
	DeadlineNano  int64
	QueueRevision uint64
	Attempt       uint64
	HasRevision   bool
	TaskID        uint64
	DesktopPatch  protocol.DesktopPreferencePatch
	EQ            eq.Settings
	View          pluginapi.Presentation
	DesktopLyrics *protocol.DesktopLyricsPreferences
}

type PluginHostReply struct {
	Failure  platformrpc.Failure
	Snapshot PluginSnapshot
	Session  *pluginapi.PlaybackSession
	Audio    *protocol.PluginAudioDescription
	Task     *protocol.DesktopTask
}

func Failure(code string, err error) platformrpc.Failure {
	if err == nil {
		return platformrpc.Failure{}
	}
	if failure, ok := err.(platformrpc.Failure); ok {
		return failure
	}
	return platformrpc.Failure{Code: code, Message: err.Error()}
}

func (r *PluginHostReply) AcceptTask(task protocol.DesktopTask, err error) error {
	if err == nil && task.ID != 0 {
		r.Task = &task
	}
	return err
}
