package app

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/waygu-music-plugin/sdk/pluginapi"

	"waygu-music/internal/player"
)

// What a song may be downloaded as is what the platform says it serves: one
// request per level, taking the format and the size that really come back.
// The answer changes only with what the listener may play, so it is kept
// for a while rather than asked again for every dialog.

// downloadOptionTTL is how long one song's answers count.
const downloadOptionTTL = 5 * time.Minute

// A level the platform would only serve below what was asked for is still
// shown, so that the listener sees why it is not on offer.
const (
	// downloadLowerOnly is what a level says when the platform serves the
	// song, but not at that level: what the listener pays for, the region
	// and the platform's own catalogue all look the same from here.
	downloadLowerOnly = "当前只能下载更低的音质"
	// downloadNeedsMembership is what a song that may not be played at
	// all says.
	downloadNeedsMembership = "需要登录或开通会员"
	downloadNotServed       = "该曲目没有这个音质"
	// downloadExists is what a song already in the download directory says.
	downloadExists = "已存在"
	// downloadAlreadyRunning is what a song already queued or being
	// fetched says when it is asked for again.
	downloadAlreadyRunning = "正在下载"
	// downloadHeld is what a song the listener already keeps says.
	downloadHeld = "已下载过了"
)

// downloadOption is one level's answer, with the stream the probe resolved,
// which the download then fetches rather than asking the platform again.
type downloadOption struct {
	option pluginapi.DownloadOption
	stream *pluginapi.Stream
}

type downloadOptionKey struct {
	platform pluginapi.ID
	ref      string
}

type downloadOptionEntry struct {
	at      time.Time
	options []downloadOption
}

// downloadOptionsCache remembers what each song may be downloaded as.
type downloadOptionsCache struct {
	mu      sync.Mutex
	entries map[downloadOptionKey]downloadOptionEntry
}

func newDownloadOptionsCache() *downloadOptionsCache {
	return &downloadOptionsCache{entries: map[downloadOptionKey]downloadOptionEntry{}}
}

// lookup answers from memory while the answer still counts, and drops the
// ones that have stopped counting.
func (c *downloadOptionsCache) lookup(key downloadOptionKey, now time.Time) ([]downloadOption, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if now.Sub(entry.at) >= downloadOptionTTL {
		delete(c.entries, key)
		return nil, false
	}
	return entry.options, true
}

func (c *downloadOptionsCache) store(key downloadOptionKey, options []downloadOption, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = downloadOptionEntry{at: now, options: options}
}

// forgetKey drops one song's answers: what it may be downloaded as has not
// changed, but whether the listener already has it has.
func (c *downloadOptionsCache) forgetKey(key downloadOptionKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// forget drops every answer: the listener's membership may have changed.
func (c *downloadOptionsCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

// downloadOptions is what one song may be downloaded as, from memory when
// it was asked lately.
func (a *app) downloadOptions(ctx context.Context, tr *pluginapi.Track) []downloadOption {
	platform, ok := a.trackPlatform(tr)
	if !ok {
		return nil
	}
	key := downloadOptionKey{platform: platform, ref: player.TrackRef(tr)}
	if options, ok := a.downloads.options.lookup(key, time.Now()); ok {
		return options
	}
	options := a.probeDownloadOptions(ctx, tr)
	if ctx.Err() != nil {
		// A probe the platform left would be missing levels for as long
		// as it is kept, so it is not kept.
		return options
	}
	a.downloads.options.store(key, options, time.Now())
	return options
}

// probeDownloadOptions asks the platform for every level, and keeps what it
// really serves: a level served under another name becomes that level's
// entry, and one served below what was asked for is shown as unavailable
// with the reason. A song the listener already keeps, or is already
// getting, has no level on offer at all: the dialog then says why rather
// than letting them ask for what the queue will refuse.
func (a *app) probeDownloadOptions(ctx context.Context, tr *pluginapi.Track) []downloadOption {
	if platform, ok := a.trackPlatform(tr); ok {
		if reason := a.downloadSkipReason(platform, tr); reason != "" {
			out := make([]downloadOption, 0, len(pluginapi.Levels))
			for _, level := range pluginapi.Levels {
				out = append(out, downloadOption{option: pluginapi.DownloadOption{
					Level: level, Available: false, Reason: reason,
				}})
			}
			return out
		}
	}
	var out []downloadOption
	for _, level := range pluginapi.Levels {
		if ctx.Err() != nil {
			return out
		}
		stream, err := a.pl.ResolveStream(ctx, tr, level)
		if err != nil || stream == nil || stream.URL == "" {
			out = append(out, downloadOption{option: pluginapi.DownloadOption{
				Level: level, Available: false, Reason: a.downloadRefusal(tr, err),
			}})
			continue
		}
		served := stream.Level
		if served == "" {
			served = level
		}
		asked, got := slices.Index(pluginapi.Levels, level), slices.Index(pluginapi.Levels, served)
		if got < 0 {
			// A level the host cannot name is one it cannot be asked to
			// download at.
			continue
		}
		if got < asked {
			out = append(out, downloadOption{option: pluginapi.DownloadOption{
				Level: level, Format: stream.Type, Size: stream.Size,
				Available: false, Reason: downloadLowerOnly,
			}})
			continue
		}
		if slices.ContainsFunc(out, func(known downloadOption) bool {
			return known.option.Available && known.option.Level == served && known.option.Format == stream.Type
		}) {
			// Two levels the platform serves alike are one choice.
			continue
		}
		out = append(out, downloadOption{option: pluginapi.DownloadOption{
			Level: served, Format: stream.Type, Size: stream.Size, Available: true,
		}, stream: stream})
	}
	return out
}

// downloadStream resolves the level one song is fetched at. Unlike the
// dialog's probe, the reminders it gives (this song is kept already, this
// song is on its way) say nothing here: the listener asked for this one
// deliberately.
func (a *app) downloadStream(ctx context.Context, tr *pluginapi.Track, level string) (*pluginapi.Stream, string) {
	stream, err := a.pl.ResolveStream(ctx, tr, level)
	if err != nil || stream == nil || stream.URL == "" {
		return nil, a.downloadRefusal(tr, err)
	}
	served := stream.Level
	if served == "" {
		served = level
	}
	if served != level {
		got, asked := slices.Index(pluginapi.Levels, served), slices.Index(pluginapi.Levels, level)
		if got >= 0 && got < asked {
			return nil, downloadLowerOnly
		}
	}
	return stream, ""
}

// downloadRefusal says why the platform gave nothing for a level: a song
// the listener may not play at all is a matter of signing in or of what
// they pay for, otherwise the platform's own words, or the song having no
// such level.
func (a *app) downloadRefusal(tr *pluginapi.Track, err error) string {
	if !tr.Playable() {
		return downloadNeedsMembership
	}
	if err != nil {
		if message := a.pl.StreamError(tr, err); message != "" {
			return message
		}
	}
	return downloadNotServed
}

// downloadStreamFor picks the stream to fetch of the level chosen, and why
// the level is not on offer when it is not.
func downloadStreamFor(options []downloadOption, level string) (*pluginapi.Stream, string) {
	for _, option := range options {
		if option.option.Level != level {
			continue
		}
		if option.option.Available && option.stream != nil {
			return option.stream, ""
		}
		if option.option.Reason != "" {
			return nil, option.option.Reason
		}
	}
	return nil, downloadNotServed
}

// downloadOptionList is the options in the shape the plugin reads.
func downloadOptionList(options []downloadOption) []pluginapi.DownloadOption {
	out := make([]pluginapi.DownloadOption, 0, len(options))
	for _, option := range options {
		out = append(out, option.option)
	}
	return out
}
