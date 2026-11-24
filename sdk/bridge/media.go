package bridge

import (
	"context"
	"net"
	"time"

	"github.com/waygu-music-plugin/sdk/protocol"
)

const (
	MaxMediaStreams     = 8
	maxPlaybackRequests = 2
	maxArtworkRequests  = MaxMediaStreams - maxPlaybackRequests
	MaxPluginCoverBytes = 16 << 20
)

// MediaSlots keeps pending artwork from using the slots needed to start playback.
type MediaSlots struct {
	artwork  chan struct{}
	playback chan struct{}
}

func NewMediaSlots() MediaSlots {
	return MediaSlots{
		artwork:  make(chan struct{}, maxArtworkRequests),
		playback: make(chan struct{}, maxPlaybackRequests),
	}
}

func (s MediaSlots) ForAction(action string) chan struct{} {
	switch action {
	case "artwork", "cover":
		return s.artwork
	default:
		return s.playback
	}
}

func ServeMedia(scope context.Context, slots chan struct{}, dial func(context.Context, uint32) (net.Conn, error), in PluginCall, out *PluginMedia, resolve func(context.Context) error) {
	if in.MediaStream == 0 {
		out.Failure = protocol.Failure{Code: "missing-media-stream"}
		return
	}
	ctx, cancel := context.WithDeadline(scope, time.Unix(0, in.DeadlineNano))
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		out.Failure = Failure("canceled", ctx.Err())
		cancel()
		return
	}
	finish := func() {
		cancel()
		<-slots
	}
	streaming := false
	defer func() {
		if !streaming {
			finish()
		}
	}()
	conn, err := dial(ctx, in.MediaStream)
	if err != nil {
		out.Failure = Failure("media-stream", err)
		return
	}
	CancelOnStreamClose(ctx, conn, cancel)

	err = ctx.Err()
	if err == nil {
		err = resolve(ctx)
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && len(out.Cover) > MaxPluginCoverBytes {
		err = protocol.Failure{Code: "cover-too-large"}
	}
	if err != nil {
		out.Cover = nil
		out.Failure = Failure("media", err)
		return
	}
	if len(out.Cover) != 0 {
		body := out.Cover
		out.Cover, out.CoverSize = nil, uint32(len(body))
		streaming = true
		go func() {
			defer finish()
			_, _ = conn.Write(body)
		}()
	}
}

// CancelOnStreamClose ties a write-only stream to the connected peer and the local context.
func CancelOnStreamClose(ctx context.Context, conn net.Conn, cancel context.CancelFunc) {
	context.AfterFunc(ctx, func() { _ = conn.Close() })
	go func() {
		var unused [1]byte
		_, _ = conn.Read(unused[:])
		cancel()
	}()
}
