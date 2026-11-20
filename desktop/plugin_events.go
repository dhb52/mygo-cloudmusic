package desktop

import (
	"context"
	"reflect"
	"time"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/protocol"
)

func (p *pluginChild) watchPlayback() {
	for p.ctx.Err() == nil {
		_ = p.consumePlayback()
		if p.ctx.Err() != nil {
			return
		}
		p.readSnapshot(0, false)
		timer := time.NewTimer(bridge.PlaybackSampleInterval)
		select {
		case <-p.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *pluginChild) consumePlayback() error {
	ctx, cancel := context.WithTimeout(p.ctx, bridge.PlaybackStreamTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var out protocol.WatchReply
	if err := p.link.Call(ctx, "Watch", protocol.WatchRequest{Scope: p.scope, DeadlineNano: deadline.UnixNano()}, &out); err != nil {
		return err
	}
	if err := out.Failure.Err(); err != nil {
		return err
	}
	conn, err := p.link.DialStream(ctx, out.Stream)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(p.ctx, func() { _ = conn.Close() })
	defer stop()
	return bridge.ReadPlaybackEvents(conn, p.scope, p.applySnapshot)
}

func (p *pluginChild) readSnapshot(revision uint64, known bool) (uint64, bool) {
	ctx, cancel := context.WithTimeout(p.ctx, time.Second)
	defer cancel()
	var reply bridge.PluginHostReply
	in := bridge.PluginHostCall{Scope: p.scope, Action: "snapshot", HasRevision: known, QueueRevision: revision}
	if err := p.callHost(ctx, in, &reply); err != nil || reply.Failure.Err() != nil {
		return revision, known
	}
	if !p.applySnapshot(reply.Snapshot) {
		return 0, false
	}
	return reply.Snapshot.QueueRevision, true
}

func (p *pluginChild) applySnapshot(snapshot bridge.PluginSnapshot) bool {
	applied := false
	mygo.RunOnMain(func() {
		if p.ctx.Err() != nil {
			return
		}
		if snapshot.UnchangedQueue {
			if snapshot.QueueRevision != p.service.state.QueueRevision {
				return
			}
			snapshot.Queue = p.service.state.Queue
			snapshot.UnchangedQueue = false
		}
		applied = true
		if reflect.DeepEqual(p.service.state, snapshot) {
			return
		}
		p.service.noteDesktopTasks(snapshot)
		p.service.state = snapshot
		p.service.updated = time.Now()
		if p.life.PlaybackChanged != nil {
			p.life.PlaybackChanged()
		}
		p.service.Invalidate()
	})
	return applied
}
