package plugintest

import (
	"context"
	"slices"
	"time"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/lyrics"
)

// Player records the playback state and commands used by plugin views.
// Transport, decoding, and refill scheduling belong to the host's own tests.
type Player struct {
	queue        []pluginapi.Track
	order        []int
	at           int
	position     time.Duration
	playing      bool
	shuffle      bool
	repeat       pluginapi.RepeatMode
	lines        []lyrics.Line
	lyricsLoaded bool
	stream       *pluginapi.Stream
	radio        string
	feed         pluginapi.QueueFeed
	cancelFeed   context.CancelFunc
}

var (
	_ pluginapi.PlaybackControls = (*Player)(nil)
	_ pluginapi.PlayerPort       = (*Player)(nil)
)

func (p *Player) Current() *pluginapi.Track {
	if len(p.order) == 0 {
		return nil
	}
	return &p.queue[p.order[p.at]]
}

func (p *Player) Playing() bool                 { return p.playing }
func (*Player) Loading() bool                   { return false }
func (p *Player) Ready() bool                   { return p.Current() != nil }
func (*Player) Err() string                     { return "" }
func (p *Player) Position() time.Duration       { return p.position }
func (*Player) Downloaded() (float64, bool)     { return 0, false }
func (p *Player) Queue() []pluginapi.Track      { return p.queue }
func (p *Player) PlayOrder() ([]int, int)       { return p.order, p.at }
func (p *Player) Shuffle() bool                 { return p.shuffle }
func (p *Player) SetShuffle(on bool)            { p.shuffle = on }
func (p *Player) ToggleShuffle()                { p.SetShuffle(!p.shuffle) }
func (p *Player) Repeat() pluginapi.RepeatMode  { return p.repeat }
func (p *Player) CycleRepeat()                  { p.repeat = (p.repeat + 1) % 3 }
func (p *Player) Radio() string                 { return p.radio }
func (p *Player) Stream() *pluginapi.Stream     { return p.stream }
func (p *Player) SetStream(s *pluginapi.Stream) { p.stream = s }
func (*Player) Levels() ([pluginapi.SpectrumBands]float32, bool) {
	return [pluginapi.SpectrumBands]float32{}, false
}

func (p *Player) Lyrics() ([]lyrics.Line, bool) { return p.lines, p.lyricsLoaded }
func (p *Player) SetLyrics(lines []lyrics.Line) { p.lines, p.lyricsLoaded = lines, true }

func (p *Player) Duration() time.Duration {
	if track := p.Current(); track != nil {
		return track.Length()
	}
	return 0
}

func (p *Player) Seek(pos time.Duration) { p.position = pos }

func (p *Player) Hold(pos time.Duration) {
	p.position, p.playing = pos, true
}

func (p *Player) TogglePlay() {
	if p.Current() != nil {
		p.playing = !p.playing
	}
}

func (p *Player) jump(at int) {
	p.at, p.position, p.playing = at, 0, true
}

func (p *Player) Next() {
	if len(p.order) > 0 {
		p.jump((p.at + 1) % len(p.order))
	}
}

func (p *Player) Prev() {
	if n := len(p.order); n > 0 {
		p.jump((p.at + n - 1) % n)
	}
}

func (p *Player) Jump(i int) {
	if at := slices.Index(p.order, i); at >= 0 {
		p.jump(at)
	}
}

func (p *Player) Reset() {
	if p.cancelFeed != nil {
		p.cancelFeed()
	}
	*p = Player{}
}

func (p *Player) Cue(tracks []pluginapi.Track) {
	p.Reset()
	p.AppendQueue(tracks)
}

func (p *Player) Restore(s pluginapi.PlaybackSession) {
	p.Reset()
	p.queue, p.order = slices.Clone(s.Queue), slices.Clone(s.Order)
	p.at, p.position = s.At, time.Duration(s.Position)*time.Millisecond
	p.shuffle, p.repeat = s.Shuffle, s.Repeat
}

func (p *Player) AppendQueue(tracks []pluginapi.Track) {
	for i := range tracks {
		p.order = append(p.order, len(p.queue)+i)
	}
	p.queue = append(p.queue, tracks...)
}

func (p *Player) PlayQueue(tracks []pluginapi.Track, start int) {
	if len(tracks) == 0 {
		return
	}
	p.Cue(tracks)
	p.jump(start)
}

func (p *Player) PlayList(tracks []pluginapi.Track, start int, feed pluginapi.QueueFeed) {
	p.PlayQueue(tracks, start)
	p.feed = feed
}

func (p *Player) PlayRadio(name string, tracks []pluginapi.Track, feed pluginapi.QueueFeed) {
	p.PlayQueue(tracks, 0)
	p.AttachRadio(name, feed)
}

func (p *Player) RadioFrom(name string, tracks []pluginapi.Track, feed pluginapi.QueueFeed) {
	if track := p.Current(); track != nil {
		p.queue = []pluginapi.Track{*track}
		p.order, p.at = []int{0}, 0
		p.AppendQueue(tracks)
		p.AttachRadio(name, feed)
	}
}

func (p *Player) AttachRadio(name string, feed pluginapi.QueueFeed) {
	p.radio, p.feed = name, feed
}

func (p *Player) EndRadio() {
	if p.radio == "" {
		return
	}
	if p.cancelFeed != nil {
		p.cancelFeed()
		p.cancelFeed = nil
	}
	p.radio, p.feed = "", nil
	if p.at+1 >= len(p.order) {
		return
	}
	kept := make([]pluginapi.Track, p.at+1)
	for i, index := range p.order[:len(kept)] {
		kept[i] = p.queue[index]
		p.order[i] = i
	}
	p.queue, p.order = kept, p.order[:len(kept)]
}

// RequestMore lets a test choose when the host asks the plugin for its next batch.
func (p *Player) RequestMore(ctx context.Context, cursor pluginapi.QueueFeedCursor) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancelFeed = cancel
	p.feed(ctx, cursor, func(tracks []pluginapi.Track, err error) {
		if ctx.Err() == nil && err == nil {
			p.AppendQueue(tracks)
		}
		cancel()
	})
}
