package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/waygu-music-plugin/sdk/pluginapi"

	"waygu-music/internal/player"
)

// downloadAdapter is a platform that serves one song at the levels a test
// chooses, and counts what it is asked for.
type downloadAdapter struct {
	streams func(context.Context, *pluginapi.Track, string) (*pluginapi.Stream, error)
	lyrics  func(context.Context, *pluginapi.Track) (string, string, string, error)
	cover   func(context.Context, *pluginapi.Track) ([]byte, error)
	asked   *int64
}

func (d downloadAdapter) Configured() bool { return true }
func (d downloadAdapter) OwnsTrack(tr *pluginapi.Track) bool {
	return tr != nil && tr.Source == "test-platform"
}
func (d downloadAdapter) FetchArtwork(context.Context, string, int) ([]byte, bool, error) {
	return nil, false, nil
}
func (d downloadAdapter) PlaybackMedia() pluginapi.PlaybackMedia {
	stream := d.streams
	if stream != nil && d.asked != nil {
		inner := stream
		stream = func(ctx context.Context, tr *pluginapi.Track, level string) (*pluginapi.Stream, error) {
			atomic.AddInt64(d.asked, 1)
			return inner(ctx, tr, level)
		}
	}
	return pluginapi.PlaybackMedia{Stream: stream, Lyrics: d.lyrics, Cover: d.cover}
}

// downloadApp is an app with one platform that answers for the test song.
func downloadApp(adapter downloadAdapter) (*app, *pluginapi.Track) {
	a := newTestApp()
	a.platformID = "test-platform"
	a.platforms = map[pluginapi.ID]pluginapi.Adapter{"test-platform": adapter}
	a.configurePlatformPlayer()
	a.pluginContext = context.Background()
	a.downloads = newDownloads()
	track := &pluginapi.Track{
		ID: 7, Name: "歌名", Source: "test-platform", Ref: "song-ref",
		Artists: []pluginapi.Artist{{Name: "歌手"}},
	}
	return a, track
}

// streamAtLevels answers each level with what the platform really serves:
// the level named, or the lower one it fell back to.
func streamAtLevels(served map[string]string, size int64) func(context.Context, *pluginapi.Track, string) (*pluginapi.Stream, error) {
	return func(_ context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		got, ok := served[level]
		if !ok {
			return nil, pluginapi.ErrUnavailable
		}
		return &pluginapi.Stream{URL: "http://example.invalid/" + got, Type: "mp3", Level: got, Size: size}, nil
	}
}

// What a song may be downloaded as is what the platform really serves, with
// the levels it would only serve lower shown as out of reach.
func TestDownloadOptionsTakeWhatThePlatformServes(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: streamAtLevels(map[string]string{
		"standard": "standard",
		"higher":   "higher",
		"exhigh":   "exhigh",
		"lossless": "standard",
	}, 4096)})

	options := a.downloadOptions(context.Background(), track)
	available := map[string]pluginapi.DownloadOption{}
	for _, option := range options {
		if option.option.Available {
			available[option.option.Level] = option.option
		}
	}
	if len(available) != 3 {
		t.Fatalf("the levels on offer are %v, want the three the platform serves", available)
	}
	for _, level := range []string{"standard", "higher", "exhigh"} {
		if option, ok := available[level]; !ok || option.Format != "mp3" || option.Size != 4096 {
			t.Fatalf("level %q is %+v", level, option)
		}
	}
	if _, ok := available["lossless"]; ok {
		t.Fatal("a level served lower than asked is on offer")
	}
	var refused pluginapi.DownloadOption
	for _, option := range options {
		if option.option.Level == "lossless" {
			refused = option.option
		}
	}
	if refused.Available || refused.Reason != downloadLowerOnly {
		t.Fatalf("the lossless level is %+v, want it refused as served lower", refused)
	}
	if stream, reason := downloadStreamFor(options, "exhigh"); stream == nil || reason != "" {
		t.Fatalf("the level on offer gave %+v, %q", stream, reason)
	}
	if stream, reason := downloadStreamFor(options, "lossless"); stream != nil || reason != downloadLowerOnly {
		t.Fatalf("the level out of reach gave %+v, %q", stream, reason)
	}
}

// Two levels the platform serves alike are one choice, and a level it
// gives nothing for says so in the platform's own words.
func TestDownloadOptionsFoldAlikeLevelsAndKeepRefusals(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: func(_ context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		switch level {
		case "standard":
			return &pluginapi.Stream{URL: "http://example.invalid/a", Type: "mp3", Level: "standard", Size: 100}, nil
		case "higher", "exhigh", "lossless", "hires":
			return &pluginapi.Stream{URL: "http://example.invalid/b", Type: "flac", Level: "lossless", Size: 900}, nil
		}
		return nil, errors.New("no")
	}})

	options := a.downloadOptions(context.Background(), track)
	var levels []string
	for _, option := range options {
		if option.option.Available {
			levels = append(levels, option.option.Level)
		}
	}
	if len(levels) != 2 || levels[0] != "standard" || levels[1] != "lossless" {
		t.Fatalf("the levels on offer are %v, want standard and lossless once each", levels)
	}
	if stream, _ := downloadStreamFor(options, "lossless"); stream == nil || stream.Type != "flac" {
		t.Fatalf("the lossless level gave %+v, want the flac the platform serves", stream)
	}
}

// The platform's own words for a refusal reach the listener; without them
// the song is said to have no such level.
func TestDownloadOptionsKeepThePlatformsReason(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: func(_ context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		if level == "standard" {
			return &pluginapi.Stream{URL: "http://example.invalid/a", Type: "mp3", Level: "standard", Size: 100}, nil
		}
		return nil, pluginapi.ErrUnavailable
	}})

	for _, option := range a.downloadOptions(context.Background(), track) {
		if option.option.Available {
			continue
		}
		if option.option.Level != "standard" && option.option.Reason != downloadNotServed {
			t.Fatalf("level %q says %q, want the song has no such level", option.option.Level, option.option.Reason)
		}
	}
}

// Asking again for the same song is answered from memory, and a platform
// that has changed forgets what it said.
func TestDownloadOptionsAreAnsweredFromMemory(t *testing.T) {
	var asked int64
	a, track := downloadApp(downloadAdapter{asked: &asked, streams: streamAtLevels(map[string]string{"standard": "standard"}, 100)})

	first := a.downloadOptions(context.Background(), track)
	once := atomic.LoadInt64(&asked)
	if once == 0 || len(first) == 0 {
		t.Fatalf("the first probe asked %d times and gave %d levels", once, len(first))
	}
	if second := a.downloadOptions(context.Background(), track); len(second) != len(first) {
		t.Fatalf("the second answer has %d levels, want %d", len(second), len(first))
	}
	twice := atomic.LoadInt64(&asked)
	if twice != once {
		t.Fatalf("the second answer asked %d more times", twice-once)
	}
	a.downloads.options.forget()
	a.downloadOptions(context.Background(), track)
	if after := atomic.LoadInt64(&asked); after <= twice {
		t.Fatal("forgetting the answers did not ask the platform again")
	}

	// An answer stops counting when it is older than it is kept for, and
	// does not stay behind in memory.
	key := downloadOptionKey{platform: "test-platform", ref: player.TrackRef(track)}
	if _, ok := a.downloads.options.lookup(key, time.Now().Add(downloadOptionTTL+time.Minute)); ok {
		t.Fatal("an answer older than its life still counts")
	}
	a.downloads.options.mu.Lock()
	held := len(a.downloads.options.entries)
	a.downloads.options.mu.Unlock()
	if held != 0 {
		t.Fatalf("%d answers are still held", held)
	}
}

// A song the listener may not play at all says so, rather than being said
// to have no such quality.
func TestDownloadOptionsSayWhyAnUnplayableSongIsRefused(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: func(context.Context, *pluginapi.Track, string) (*pluginapi.Stream, error) {
		return nil, pluginapi.ErrUnavailable
	}})
	track.Privilege = &pluginapi.Privilege{St: 0, Pl: 0}

	options := a.downloadOptions(context.Background(), track)
	if len(options) == 0 {
		t.Fatal("the probe answered nothing at all")
	}
	for _, option := range options {
		if option.option.Available || option.option.Reason != downloadNeedsMembership {
			t.Fatalf("level %q is %+v, want it refused for what the listener may play", option.option.Level, option.option)
		}
	}
}

// A level the host cannot name is not offered: it could not be asked to
// download at it.
func TestDownloadOptionsDropLevelsTheHostCannotName(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: func(context.Context, *pluginapi.Track, string) (*pluginapi.Stream, error) {
		return &pluginapi.Stream{URL: "http://example.invalid/a", Type: "mp3", Level: "ultra", Size: 10}, nil
	}})
	if options := a.downloadOptions(context.Background(), track); len(options) != 0 {
		t.Fatalf("the probe offered %+v", options)
	}
}

// A probe the platform leaves ends rather than running on.
func TestDownloadOptionsStopWithTheContext(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: func(ctx context.Context, _ *pluginapi.Track, level string) (*pluginapi.Stream, error) {
		if level == "standard" {
			return &pluginapi.Stream{URL: "http://example.invalid/a", Type: "mp3", Level: "standard", Size: 100}, nil
		}
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if options := a.downloadOptions(ctx, track); len(options) != 0 {
		t.Fatalf("a canceled probe gave %d levels", len(options))
	}
}

// The host tells the plugin that downloads are on offer, where they go, and
// what the last probe answered.
func TestDesktopSettingsAdvertiseDownloads(t *testing.T) {
	a, track := downloadApp(downloadAdapter{streams: streamAtLevels(map[string]string{"standard": "standard"}, 100)})
	dir := t.TempDir()
	a.settings.DownloadDir = dir

	state := platformHost{a: a}.DesktopSettings()
	if !state.DownloadAvailable || state.DownloadDir != dir {
		t.Fatalf("the host advertises downloads=%v in %q, want true in %q", state.DownloadAvailable, state.DownloadDir, dir)
	}
	options := a.downloadOptions(context.Background(), track)
	a.downloads.setOptions("token-1", options)

	state = platformHost{a: a}.DesktopSettings()
	if len(state.DownloadOptions) == 0 || state.DownloadOptionsToken != "token-1" {
		t.Fatalf("the probe answer is %d options under %q", len(state.DownloadOptions), state.DownloadOptionsToken)
	}
}
