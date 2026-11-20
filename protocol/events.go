package protocol

// WatchRequest opens a playback stream for an already active scope.
type WatchRequest struct {
	Scope        Scope
	DeadlineNano int64
}

type WatchReply struct {
	Failure Failure
	Stream  uint32
}

// PlaybackEvent carries an ordered snapshot. The first event on each stream
// carries the full queue. Later events can omit an unchanged queue.
type PlaybackEvent struct {
	Scope    Scope
	Sequence uint64
	Snapshot PluginSnapshot
}
