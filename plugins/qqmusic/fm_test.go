package qqmusic

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	backend "github.com/waygu-music-plugin/qqmusic/internal/client"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/plugintest"
)

type fmTestHost struct {
	commentTestHost
	pl *plugintest.Player
}

func (h fmTestHost) Player() PlayerPort                   { return h.pl }
func (h fmTestHost) Playback() pluginapi.PlaybackControls { return h.pl }
func (fmTestHost) Artwork(string, int) (*ui.Bitmap, bool) { return nil, false }
func (fmTestHost) DecodedArtwork(string, int, bool) pluginapi.DecodedArtwork {
	return nil
}
func (fmTestHost) Invalidate()  {}
func (fmTestHost) Toast(string) {}

// Starting QQ Music FM refills its queue, and leaving cancels the next request without changing the current song.
func TestQQFMRefillsAndCancelsOnExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	pl := &plugintest.Player{}
	t.Cleanup(pl.Reset)
	host := fmTestHost{commentTestHost{pending: make(chan func(), 4)}, pl}
	home := NewHome(NewState(), host)
	home.scope = ctx
	previous := loadPersonalFM
	defer func() { loadPersonalFM = previous }()
	type request struct {
		ctx   context.Context
		reply chan backend.Track
	}
	started := make(chan request, 3)
	loadPersonalFM = func(ctx context.Context, _ string) ([]backend.Track, error) {
		reply := make(chan backend.Track, 1)
		started <- request{ctx, reply}
		select {
		case track := <-reply:
			return []backend.Track{track}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	nextRequest := func() request {
		t.Helper()
		select {
		case req := <-started:
			return req
		case <-ctx.Done():
			t.Fatal("the radio did not request a batch")
			return request{}
		}
	}
	applyResult := func() {
		t.Helper()
		select {
		case fn := <-host.pending:
			fn()
		case <-ctx.Done():
			t.Fatal("the radio did not deliver its batch")
		}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		home.fmCard(c)
		if name := pl.Radio(); name != "" {
			home.radioChip(c, name)
		}
	}, 1200, 800)
	if err := tt.Click("开始收听"); err != nil {
		t.Fatal(err)
	}
	nextRequest().reply <- backend.Track{ID: 111, MID: "fm-first", Name: "First"}
	applyResult()
	pl.RequestMore(ctx, pluginapi.QueueFeedCursor{FirstID: 111, LastID: 111})
	nextRequest().reply <- backend.Track{ID: 112, MID: "fm-refill", Name: "Refill"}
	applyResult()
	if pl.Radio() != fmRadio || len(pl.Queue()) != 2 || pl.Queue()[1].Ref != "fm-refill" {
		t.Fatalf("FM did not append its batch: %s, %v", pl.Radio(), pl.Queue())
	}
	tt.Frame()
	if err := tt.Click("下一首"); err != nil {
		t.Fatal(err)
	}
	pl.RequestMore(ctx, pluginapi.QueueFeedCursor{Page: 1, FirstID: 111, LastID: 112})
	pending := nextRequest()
	tt.Frame()
	if err := tt.Click("退出私人 FM"); err != nil {
		t.Fatal(err)
	}
	if pending.ctx.Err() != context.Canceled || pl.Radio() != "" || pl.Current().ID != 112 {
		t.Fatalf("leaving FM did not cancel the request and keep its current song: %v, %q, %v", pending.ctx.Err(), pl.Radio(), pl.Current())
	}
	applyResult()
	if len(pl.Queue()) != 2 {
		t.Fatalf("late refill changed the queue: %v", pl.Queue())
	}
}
