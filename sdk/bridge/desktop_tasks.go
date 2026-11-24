package bridge

import "github.com/waygu-music-plugin/sdk/protocol"

func TaskMessage(task protocol.DesktopTask) string {
	if task.Phase == protocol.TaskCanceled && task.Kind == protocol.TaskDecoderDownload {
		return "已取消下载解码组件"
	}
	if task.Phase == protocol.TaskFailed {
		return task.Message
	}
	if task.Phase == protocol.TaskSucceeded {
		switch task.Kind {
		case protocol.TaskDecoderDownload:
			return "高级解码组件下载完成"
		case protocol.TaskDecoderDelete:
			return "已删除高级解码组件"
		case protocol.TaskCacheClear:
			return "已清除歌曲缓存"
		case protocol.TaskCastingOutput:
			return "已选择播放输出"
		}
	}
	return ""
}
