package desktop

import (
	"context"
	"time"

	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/pluginapi"
)

type pluginChildContinuity struct{ *pluginClientServices }

func (p pluginChildContinuity) LoadSession() (pluginapi.PlaybackSession, bool) {
	ctx, cancel := context.WithTimeout(p.child.ctx, 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	in := bridge.PluginHostCall{Action: "session-load", Scope: p.child.scope, DeadlineNano: deadline.UnixNano()}
	var out bridge.PluginHostReply
	if err := p.child.callHost(ctx, in, &out); err != nil || out.Failure.Err() != nil || out.Session == nil {
		return pluginapi.PlaybackSession{}, false
	}
	return *out.Session, true
}

func (p pluginChildContinuity) Restore(s pluginapi.PlaybackSession) {
	p.send(bridge.PluginHostCall{Action: "session-restore", Session: &s})
}

func (p pluginChildContinuity) RadioFrom(name string, tracks []pluginapi.Track, more pluginapi.QueueFeed) {
	p.send(bridge.PluginHostCall{Action: "radio-from", Text: name, Tracks: tracks, Feed: p.attachFeed(more)})
}

func (p pluginChildContinuity) AttachRadio(name string, more pluginapi.QueueFeed) {
	p.send(bridge.PluginHostCall{Action: "radio-attach", Text: name, Feed: p.attachFeed(more)})
}

func (p pluginChildContinuity) OpenAudio() (pluginapi.Audio, error) {
	ctx, cancel := context.WithTimeout(p.child.ctx, 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	in := bridge.PluginHostCall{Action: "audio-open", Scope: p.child.scope, DeadlineNano: deadline.UnixNano()}
	var out bridge.PluginHostReply
	if err := p.child.callHost(ctx, in, &out); err != nil {
		return nil, err
	}
	if err := out.Failure.Err(); err != nil {
		return nil, err
	}
	if out.Audio == nil || out.Audio.Rate <= 0 || out.Audio.Channels <= 0 {
		return nil, pluginapi.ErrUnavailable
	}
	conn, err := p.child.link.DialStream(ctx, out.Audio.ID)
	if err != nil {
		return nil, err
	}
	return bridge.NewAudio(*out.Audio, conn), nil
}
