package protocol

type TaskPhase string

const (
	TaskRunning   TaskPhase = "running"
	TaskSucceeded TaskPhase = "succeeded"
	TaskFailed    TaskPhase = "failed"
	TaskCanceled  TaskPhase = "canceled"
	TaskReplaced  TaskPhase = "replaced"
)

// DesktopTask describes one host operation. ID is unique until the host exits.
// A zero ID means this operation has no recorded task.
type DesktopTask struct {
	ID      uint64
	Kind    string
	Phase   TaskPhase
	Message string
}

func (t DesktopTask) Running() bool { return t.Phase == TaskRunning }

const (
	TaskDecoderDownload = "decoder-download"
	TaskDecoderDelete   = "decoder-delete"
	TaskCacheClear      = "clear-cache"
	TaskCastingScan     = "cast-scan"
	TaskCastingOutput   = "cast-output"
	TaskSongDownload    = "song-download"
	HostDesktopTask     = "desktop-task"
)
