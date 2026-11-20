package desktop

import (
	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/protocol"
)

func taskResults(s bridge.PluginSnapshot) [5]protocol.DesktopTask {
	return [5]protocol.DesktopTask{
		s.Desktop.CacheTask, s.Decoder.DownloadTask, s.Decoder.DeleteTask,
		s.Casting.ScanTask, s.Casting.OutputTask,
	}
}

func (s *pluginClientServices) noteDesktopTasks(snapshot bridge.PluginSnapshot) {
	previous := taskResults(s.state)
	for i, task := range taskResults(snapshot) {
		if task.ID != 0 && task != previous[i] {
			if message := bridge.TaskMessage(task); message != "" {
				s.toast = message
			}
		}
	}
}
