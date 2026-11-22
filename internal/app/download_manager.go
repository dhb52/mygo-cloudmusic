package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// The download manager shows what the host is fetching, what waits, and
// what the listener has kept: the queue lives here, not in any one
// platform's window, so that every platform shares it.

// downloadRowGap is how much room one row of the kept list takes, for the
// list to know how many to build.
const downloadRowGap = 54

func (a *app) openDownloadManager() {
	a.dismissStartupNotice()
	if a.downloadsWin != nil {
		a.downloadsWin.Show()
		a.downloadsWin.Focus()
		a.refreshDownloadRecords()
		return
	}
	a.refreshDownloadRecords()
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "下载管理器", Width: 720, Height: 560, MinWidth: 520, MinHeight: 380,
		Content: ui.View(a.downloadManagerView),
	})
	a.downloadsWin = win
	_ = win.SetIcon(desktopApplicationIcon())
	a.syncDock()
	win.SetMenu(mygo.NewMenu(nil))
	win.OnClosed(func() {
		a.downloadsWin = nil
		a.syncDock()
	})
	win.Focus()
}

// refreshDownloadRecords takes the kept list once, rather than on every
// frame the window draws.
func (a *app) refreshDownloadRecords() {
	if a.downloads == nil || a.downloads.records == nil {
		return
	}
	a.downloadedList = a.downloads.records.list()
}

func (a *app) downloadManagerView(c *ui.Context) {
	theme := *c.Theme()
	p := managerColors(theme.Dark)
	theme.Background, theme.Text, theme.TextMuted = p.background, p.ink, p.muted
	theme.Surface, theme.SurfaceHover, theme.SurfacePressed = p.row, p.active, p.border
	theme.Border, theme.Accent, theme.Focus = p.border, p.accent, p.accent
	theme.AccentText = ui.RGB(255, 255, 255)
	if theme.Dark {
		theme.AccentText = ui.RGB(24, 45, 36)
	}
	theme.AccentHover = p.accent.Mix(ui.RGB(255, 255, 255), 0.10)
	theme.AccentPressed = p.accent.Mix(ui.RGB(0, 0, 0), 0.12)
	theme.Selection = p.accent.Alpha(0.26)
	c.SetTheme(&theme)
	ui.Scroll(c).Fill().Padding(22).Gap(16).Children(func() {
		a.downloadManagerHeader(c, p)
		a.downloadManagerActive(c, p)
		a.downloadManagerKept(c, p)
		a.downloadManagerDone(c, p)
	})
}

func (a *app) downloadManagerHeader(c *ui.Context, p pluginManagerColors) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "下载").Grow(1).FontSize(24).FontWeight(700).TextColor(p.ink)
	})
	dir, err := a.downloadDir()
	if err != nil {
		dir = ""
	}
	description := dir
	if description == "" {
		description = "还没有可用的下载目录"
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, description).FontSize(12).TextColor(p.muted).Grow(1).Shrink(1).SingleLine()
		if ui.Button(c, "更改…").Clicked() {
			a.chooseDownloadDir()
		}
		if ui.Button(c, "打开目录").Disabled(dir == "").Clicked() {
			if err := mygo.Shell.OpenPath(dir); err != nil {
				a.toast = err.Error()
			}
		}
	})
}

// downloadManagerActive lists what is being fetched and what waits.
func (a *app) downloadManagerActive(c *ui.Context, p pluginManagerColors) {
	items := downloadItemsWithPhase(a.downloads.snapshot(), protocol.TaskRunning)
	a.downloadManagerSection(c, p, "正在下载", len(items), func() {
		if len(items) == 0 {
			a.downloadManagerEmpty(c, p, "没有正在下载的歌曲")
			return
		}
		if ui.Button(c, "全部取消").Clicked() {
			a.cancelAllDownloads()
		}
		for i, item := range items {
			a.downloadManagerActiveRow(c, p, item, i)
		}
	})
}

func (a *app) downloadManagerActiveRow(c *ui.Context, p pluginManagerColors, item pluginapi.DownloadItem, index int) {
	ui.Row(c).FillWidth().Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, item.Name).FontSize(13.5).FontWeight(500).TextColor(p.ink).SingleLine()
			ui.Text(c, downloadProgressText(item, index)).FontSize(12).TextColor(p.muted)
		})
		if ui.Button(c, "取消").Clicked() {
			a.stopDownload(item.ID, "")
		}
	})
}

// downloadProgressText says how far one song has come, or that it is
// waiting its turn.
func downloadProgressText(item pluginapi.DownloadItem, index int) string {
	level := downloadLevelLabel(item.Level)
	if item.Total > 0 {
		share := float64(item.Done) / float64(item.Total)
		return fmt.Sprintf("%s · %d%%（%s / %s）", level, int(share*100+0.5), megabytes(item.Done), megabytes(item.Total))
	}
	if index > 0 {
		return level + " · 排队中"
	}
	return level + " · 正在获取"
}

// downloadManagerKept lists what the listener has.
func (a *app) downloadManagerKept(c *ui.Context, p pluginManagerColors) {
	records := a.downloadedList
	a.downloadManagerSection(c, p, fmt.Sprintf("已下载（%d）", len(records)), len(records), func() {
		if len(records) == 0 {
			a.downloadManagerEmpty(c, p, "下载过的歌曲会出现在这里")
			return
		}
		if ui.Button(c, "清空列表").Clicked() {
			a.clearDownloadRecords()
		}
		_, height := c.Size()
		rows := max(int(height/downloadRowGap)+2, 1)
		ui.List(c.Key("downloads-kept"), &a.downloadList, min(rows, len(records)), func(i int) {
			a.downloadManagerKeptRow(c, p, records[i])
		}).Height(float32(min(len(records), rows)) * downloadRowGap)
	})
}

func (a *app) downloadManagerKeptRow(c *ui.Context, p pluginManagerColors, record downloadRecord) {
	missing := record.Path == ""
	ui.Row(c).FillWidth().Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, record.Name).FontSize(13.5).FontWeight(500).TextColor(p.ink).SingleLine()
			ui.Text(c, downloadRecordDetail(record, missing)).FontSize(12).TextColor(p.muted).SingleLine()
		})
		if missing {
			if ui.Button(c, "从列表移除").Clicked() {
				a.forgetDownload(record)
			}
		} else if ui.Button(c, "打开文件").Clicked() {
			if _, err := os.Stat(record.Path); err != nil {
				a.toast = "找不到下载的文件"
			} else {
				mygo.Shell.ShowItemInFolder(record.Path)
			}
		}
		if ui.Button(c, "重新下载").Clicked() {
			a.redownloadDownload(record)
		}
	})
}

// downloadRecordDetail says what one kept song is, and that its file is
// gone when it is.
func downloadRecordDetail(record downloadRecord, missing bool) string {
	if missing {
		return downloadLevelLabel(record.Level) + " · 文件已不在"
	}
	return fmt.Sprintf("%s · %s · %s", downloadLevelLabel(record.Level), megabytes(record.Size), downloadAge(record.At))
}

// downloadManagerDone lists what failed or was stopped lately.
func (a *app) downloadManagerDone(c *ui.Context, p pluginManagerColors) {
	var items []pluginapi.DownloadItem
	for _, item := range a.downloads.snapshot() {
		if item.Phase == protocol.TaskFailed || item.Phase == protocol.TaskCanceled {
			items = append(items, item)
		}
	}
	a.downloadManagerSection(c, p, "失败与已取消", len(items), func() {
		if len(items) == 0 {
			a.downloadManagerEmpty(c, p, "没有失败的下载")
			return
		}
		for _, item := range items {
			ui.Row(c).FillWidth().Padding(10, 12).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, item.Name).FontSize(13.5).FontWeight(500).TextColor(p.ink).SingleLine()
					detail := "已取消"
					if item.Phase == protocol.TaskFailed {
						detail = item.Message
						if detail == "" {
							detail = "下载失败"
						}
					}
					ui.Text(c, downloadLevelLabel(item.Level)+" · "+detail).FontSize(12).TextColor(p.muted).SingleLine()
				})
				if ui.Button(c, "重试").Clicked() {
					if err := a.retryDownload(item.ID); err != nil {
						a.toast = err.Error()
					}
				}
			})
		}
	})
}

func (a *app) downloadManagerSection(c *ui.Context, p pluginManagerColors, title string, count int, rows func()) {
	ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(p.muted).Margin(2, 2, 0, 0)
	ui.Column(c).FillWidth().Background(p.panel).Border(1, p.border).Radius(14).Padding(6).Gap(4).Children(rows)
}

func (a *app) downloadManagerEmpty(c *ui.Context, p pluginManagerColors, text string) {
	ui.Text(c, text).FontSize(12.5).TextColor(p.muted).Padding(8, 10)
}

// downloadItemsWithPhase picks the items in one phase, in the order given:
// the song being fetched comes first, the ones waiting after it.
func downloadItemsWithPhase(items []pluginapi.DownloadItem, phase protocol.TaskPhase) []pluginapi.DownloadItem {
	var out []pluginapi.DownloadItem
	for _, item := range items {
		if item.Phase == phase {
			out = append(out, item)
		}
	}
	return out
}

// clearDownloadRecords forgets every kept song, leaving the files.
func (a *app) clearDownloadRecords() {
	a.downloads.records.clear()
	a.refreshDownloadRecords()
	a.desktopTasksChanged()
}

// forgetDownload drops one kept song from the list.
func (a *app) forgetDownload(record downloadRecord) {
	a.downloads.records.forget(record.key())
	a.refreshDownloadRecords()
	a.desktopTasksChanged()
}

// redownloadDownload fetches one kept song again, as a copy of its own: the
// listener asked for it, so the record no longer stands in its way.
func (a *app) redownloadDownload(record downloadRecord) {
	a.forgetDownload(record)
	if err := a.startDownloads([]pluginapi.Track{record.track()}, record.Level); err != nil {
		a.toast = err.Error()
	}
}

// retryDownload fetches one failed or stopped song again.
func (a *app) retryDownload(id uint64) error {
	job := a.downloads.finished(id)
	if job == nil {
		return errors.New("找不到这个下载")
	}
	return a.startDownloads([]pluginapi.Track{job.track}, job.item.Level)
}

// finished is the download that ended with this id, if it is still listed.
func (d *downloads) finished(id uint64) *downloadJob {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, done := range d.recent {
		if done.item.ID == id {
			return done.job
		}
	}
	return nil
}

// cancelAllDownloads stops every song waiting and the one being fetched.
func (a *app) cancelAllDownloads() {
	d := a.downloads
	d.mu.Lock()
	ids := make([]uint64, 0, len(d.queued)+1)
	for _, job := range d.queued {
		ids = append(ids, job.item.ID)
	}
	if d.running != nil {
		ids = append(ids, d.running.item.ID)
	}
	d.mu.Unlock()
	for _, id := range ids {
		a.stopDownload(id, "")
	}
}

// stopDownload stops one song's download: the one being fetched now, or one
// still waiting. An empty from stops it whatever platform it belongs to,
// which is what the manager does.
func (a *app) stopDownload(id uint64, from pluginapi.ID) error {
	d := a.downloads
	d.mu.Lock()
	if d.running != nil && d.running.item.ID == id {
		if from != "" && d.running.platform != from {
			d.mu.Unlock()
			return errors.New("不是这个平台的下载")
		}
		cancel := d.running.cancel
		d.mu.Unlock()
		if cancel == nil {
			return errors.New("这个下载还没有开始")
		}
		cancel()
		return nil
	}
	for i, job := range d.queued {
		if job.item.ID != id {
			continue
		}
		if from != "" && job.platform != from {
			d.mu.Unlock()
			return errors.New("不是这个平台的下载")
		}
		d.queued = append(d.queued[:i], d.queued[i+1:]...)
		d.mu.Unlock()
		a.finishDownload(job, protocol.TaskCanceled, "")
		return nil
	}
	d.mu.Unlock()
	return errors.New("找不到这个下载")
}

// chooseDownloadDir asks the listener where downloads should go, and keeps
// the answer where the platforms read it.
func (a *app) chooseDownloadDir() {
	current, err := a.downloadDir()
	if err != nil {
		current = ""
	}
	go func() {
		picked, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title: "选择下载目录", DefaultPath: current, Directory: true, CreateDirectories: true,
			ButtonLabel: "选择",
		})
		mygo.RunOnMain(func() {
			if err != nil || len(picked) == 0 {
				if err != nil {
					a.toast = err.Error()
					a.redraw()
				}
				return
			}
			if err := a.setDownloadDir(picked[0]); err != nil {
				a.toast = err.Error()
			}
			a.refreshDownloadRecords()
			a.redraw()
		})
	}()
}

// setDownloadDir writes the download directory into the preferences, the
// way the plugins write them.
func (a *app) setDownloadDir(dir string) error {
	next := a.settings
	next.DownloadDir = dir
	if err := next.save(); err != nil {
		return err
	}
	a.settings = next
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
	a.desktopTasksChanged()
	return nil
}

// megabytes is a size as the manager shows it, to one decimal.
func megabytes(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// downloadAge is how long ago something was, in the manager's words.
func downloadAge(at time.Time) string {
	elapsed := time.Since(at)
	switch {
	case elapsed < time.Minute:
		return "刚刚"
	case elapsed < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(elapsed.Hours()))
	}
	return at.Format("2006-01-02")
}
