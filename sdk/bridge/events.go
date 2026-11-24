package bridge

import (
	"encoding/gob"
	"errors"
	"net"
	"time"

	"github.com/waygu-music-plugin/sdk/protocol"
)

const (
	PlaybackSampleInterval = 250 * time.Millisecond
	PlaybackStreamTimeout  = 5 * time.Second
)

func ReadPlaybackEvents(conn net.Conn, scope protocol.Scope, apply func(PluginSnapshot) bool) error {
	decoder := gob.NewDecoder(conn)
	var sequence, revision uint64
	for {
		if err := conn.SetReadDeadline(time.Now().Add(PlaybackStreamTimeout)); err != nil {
			return err
		}
		var event protocol.PlaybackEvent
		if err := decoder.Decode(&event); err != nil {
			return err
		}
		if event.Scope != scope || event.Sequence == 0 || (sequence != 0 && event.Sequence != sequence+1) {
			return errors.New("unexpected playback event scope or sequence")
		}
		if event.Snapshot.UnchangedQueue && (sequence == 0 || event.Snapshot.QueueRevision != revision) {
			return errors.New("playback event omitted an unknown queue")
		}
		if !apply(LocalSnapshot(event.Snapshot)) {
			return errors.New("playback snapshot could not be applied")
		}
		sequence, revision = event.Sequence, event.Snapshot.QueueRevision
	}
}
