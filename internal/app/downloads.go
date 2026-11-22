package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"

	"waygu-music/internal/player"
)

// A download is a song the listener keeps: the host fetches it, names it,
// and writes its lyric and cover beside it, so that every platform is
// served the same way. One comes at a time, in the order asked for.

const (
	// downloadRecent is how many finished downloads the settings window
	// shows.
	downloadRecent = 5
	// downloadProgressStep is how much of a song must come in before what
	// is shown moves on, so that a fast download does not redraw for
	// every buffer.
	downloadProgressStep = 1 << 20
	// maxDownloadTracks is the most songs one batch may hold, which is
	// what the host lets a plugin hand over at once.
	maxDownloadTracks = 8192
)

// downloadJob is one song's download, waiting or being fetched.
type downloadJob struct {
	item     pluginapi.DownloadItem
	platform pluginapi.ID
	track    pluginapi.Track
	// dir is where the batch's files go, settled when it was queued so
	// that the worker never reads the settings off its own thread.
	dir string
	// reason says why a song that is not fetched anyway was asked for.
	reason string
	cancel context.CancelFunc
}

// downloadTally counts how a batch went.
type downloadTally struct{ succeeded, skipped, failed, canceled int }

// finishedDownload is one that is over, kept with the song it was of, so
// that the listener can ask for it again.
type finishedDownload struct {
	job  *downloadJob
	item pluginapi.DownloadItem
}

// downloads is what is being fetched, what waits, and what finished lately,
// which the settings window shows. The application thread changes it; the
// worker posts what it finds.
type downloads struct {
	options *downloadOptionsCache
	records *downloadRecords
	wake    chan struct{}

	mu        sync.Mutex
	next      uint64
	nextBatch uint64
	running   *downloadJob
	queued    []*downloadJob
	recent    []finishedDownload
	// token and optionList are the probe answered last, for the platform's
	// dialog to pick up by its token.
	token      string
	optionList []pluginapi.DownloadOption
	// left counts the songs of each batch that have not ended, and task is
	// the desktop task the settings window shows for the batch.
	left    map[uint64]int
	task    map[uint64]protocol.DesktopTask
	tally   map[uint64]downloadTally
	batched map[uint64]bool
}

func newDownloads() *downloads {
	return &downloads{
		options: newDownloadOptionsCache(),
		records: newDownloadRecords(),
		wake:    make(chan struct{}, 1),
		left:    map[uint64]int{},
		task:    map[uint64]protocol.DesktopTask{},
		tally:   map[uint64]downloadTally{},
		batched: map[uint64]bool{},
	}
}

// cachedDownload is where the cache already holds the song of this level
// and format, so that a download copies it rather than fetching it again.
// A song kept at another level is not it: the copy would be named for the
// level asked for while holding the other one.
var cachedDownload = func(cache *player.Cache, platform pluginapi.ID, tr *pluginapi.Track, level, format string) (string, bool) {
	if cache == nil {
		return "", false
	}
	path, kept, ok := cache.CachedPath(platform, tr, level)
	if !ok || kept.Type != format {
		return "", false
	}
	return path, true
}

// serveDownloads fetches what was asked for, one song at a time, so that a
// slow song neither walks over the network nor over the interface.
func (a *app) serveDownloads() {
	for range a.downloads.wake {
		for {
			job, ctx, cancel := a.downloads.take(a.pluginContext)
			if job == nil {
				break
			}
			a.runDownload(job, ctx, cancel)
		}
	}
}

// startDownloads queues the songs of one batch, all at the level chosen.
func (a *app) startDownloads(tracks []pluginapi.Track, level string) error {
	if len(tracks) == 0 {
		return errors.New("没有要下载的歌曲")
	}
	if len(tracks) > maxDownloadTracks {
		return fmt.Errorf("一次最多下载 %d 首", maxDownloadTracks)
	}
	if !slices.Contains(pluginapi.Levels, level) {
		return fmt.Errorf("未知的音质：%s", level)
	}
	// Everything that would fail at once is settled before the batch is
	// opened, so that no song is ever queued without one. A song the
	// listener has already kept, or is already getting, is not fetched
	// twice: it is put in the list as done, saying why.
	type planned struct {
		track  pluginapi.Track
		owner  pluginapi.ID
		reason string
	}
	plans := make([]planned, 0, len(tracks))
	for i := range tracks {
		platform, ok := a.trackPlatform(&tracks[i])
		if !ok {
			return fmt.Errorf("找不到 %s 所属的平台", tracks[i].Name)
		}
		plans = append(plans, planned{track: tracks[i], owner: platform, reason: a.downloadSkipReason(platform, &tracks[i])})
		// What a song is on offer as is answered with whether the
		// listener has it, which is about to change.
		a.downloads.options.forgetKey(downloadOptionKey{platform: platform, ref: player.TrackRef(&tracks[i])})
	}
	dir, err := a.downloadDir()
	if err != nil {
		return err
	}
	if err := writableDir(dir); err != nil {
		return err
	}
	d := a.downloads
	// The settings window shows one batch at a time: the one before it is
	// over as far as it is concerned.
	if running := a.desktopTasks.latest[protocol.TaskSongDownload]; running.Running() {
		a.finishDesktopTask(running, protocol.TaskReplaced, nil)
	}
	batch := d.openBatch()
	task := a.beginDesktopTask(protocol.TaskSongDownload)
	for _, plan := range plans {
		job := &downloadJob{
			item: pluginapi.DownloadItem{
				Batch: batch, Name: player.SongName(&plan.track), Level: level,
				Ref: player.TrackRef(&plan.track), Phase: protocol.TaskRunning,
			},
			platform: plan.owner,
			track:    plan.track,
			dir:      dir,
			reason:   plan.reason,
		}
		if plan.reason != "" {
			d.addSkipped(job, task)
			continue
		}
		d.add(job, task)
	}
	a.closeDownloadBatch(batch)
	for _, plan := range plans {
		if plan.reason == "" {
			continue
		}
		a.toast = downloadToast(pluginapi.DownloadItem{
			Name: player.SongName(&plan.track), Phase: protocol.TaskSucceeded, Message: plan.reason,
		})
	}
	d.wakeUp()
	return nil
}

// downloadSkipReason says why a song is not fetched: it is on its way
// already, or the listener has it. Nothing is fetched twice.
func (a *app) downloadSkipReason(platform pluginapi.ID, tr *pluginapi.Track) string {
	key := downloadRecordKey{platform: platform, ref: player.TrackRef(tr)}
	switch {
	case a.downloads.queuedOrRunning(key):
		return downloadAlreadyRunning
	}
	if _, ok := a.downloads.records.find(key); ok {
		return downloadHeld
	}
	return ""
}

// cancelDownload stops one song's download, when the platform asking is the
// one it belongs to.
func (a *app) cancelDownload(id uint64, from pluginapi.ID) error {
	return a.stopDownload(id, from)
}

// runDownload fetches one song with its lyric and cover. It runs off the
// application thread; what it finds reaches the interface through post.
func (a *app) runDownload(job *downloadJob, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	// The slot goes back on this thread, not on the application thread: a
	// batch's next song must not wait for the interface to catch up.
	defer a.downloads.release(job)

	stream, reason := a.downloadStream(ctx, &job.track, job.item.Level)
	if stream == nil {
		a.finishDownload(job, downloadPhase(ctx, protocol.TaskFailed), downloadReason(ctx, reason))
		return
	}
	target, err := player.ReserveTarget(job.dir, &job.track, stream.Type, stream.Size)
	if err != nil {
		a.finishDownload(job, downloadPhase(ctx, protocol.TaskFailed), downloadReason(ctx, err.Error()))
		return
	}
	if target.Exists {
		a.finishDownloadAt(job, protocol.TaskSucceeded, downloadExists, target.Path)
		return
	}

	var lastPosted int64
	progress := func(done, total int64) {
		if done-lastPosted < downloadProgressStep && done != total {
			return
		}
		lastPosted = done
		postDesktopTask(a, func() { a.downloads.setProgress(job, done, total) })
	}
	if path, ok := cachedDownload(a.pl.Cache, job.platform, &job.track, job.item.Level, stream.Type); ok {
		_, err = player.CopyFile(path, target.Path)
	} else {
		_, err = player.DownloadFile(ctx, stream, target.Path, progress)
	}
	if err != nil {
		if ctx.Err() != nil {
			a.finishDownload(job, protocol.TaskCanceled, "")
			return
		}
		a.finishDownload(job, protocol.TaskFailed, err.Error())
		return
	}
	a.writeDownloadSidecars(ctx, &job.track, target)
	a.finishDownloadAt(job, protocol.TaskSucceeded, "", target.Path)
}

// downloadPhase is how a song that could not be fetched ends: a listener
// who stopped it sees it canceled, whatever it was doing at the time.
func downloadPhase(ctx context.Context, phase protocol.TaskPhase) protocol.TaskPhase {
	if ctx.Err() != nil {
		return protocol.TaskCanceled
	}
	return phase
}

// downloadReason drops the reason of a failure that was really a cancel.
func downloadReason(ctx context.Context, reason string) string {
	if ctx.Err() != nil {
		return ""
	}
	return reason
}

// writeDownloadSidecars puts the lyric and the cover beside a song that has
// come in. Neither is worth failing the download over.
func (a *app) writeDownloadSidecars(ctx context.Context, tr *pluginapi.Track, target player.Target) {
	dir := filepath.Dir(target.Path)
	if lyric, translation, _, err := a.pl.ResolveLyrics(ctx, tr); err == nil {
		_, _ = player.WriteLyrics(dir, target.Base, lyric, translation)
	}
	if ctx.Err() != nil {
		return
	}
	if cover, err := a.pl.Artwork(ctx, tr); err == nil && len(cover) > 0 {
		_, _ = player.WriteCover(dir, target.Base, cover)
	}
}

// finishDownload records how one song went, where the listener can see it.
func (a *app) finishDownload(job *downloadJob, phase protocol.TaskPhase, message string) {
	a.finishDownloadAt(job, phase, message, "")
}

func (a *app) finishDownloadAt(job *downloadJob, phase protocol.TaskPhase, message, path string) {
	item := job.item
	item.Phase, item.Path, item.Message = phase, path, message
	postDesktopTask(a, func() {
		skipped := phase == protocol.TaskSucceeded && item.Message != ""
		a.downloads.finish(job, item, skipped)
		a.recordDownload(job, item)
		a.downloads.options.forgetKey(downloadOptionKey{platform: job.platform, ref: player.TrackRef(&job.track)})
		// The batch that ends with this song says how it all went, which
		// the listener would rather see than the last song alone.
		summary, closed := a.closeDownloadBatch(job.item.Batch)
		if closed && summary != "" {
			a.toast = summary
		} else {
			a.toast = downloadToast(item)
		}
		a.desktopTasksChanged()
	})
}

// downloadToast is what the listener is told about one download.
func downloadToast(item pluginapi.DownloadItem) string {
	switch item.Phase {
	case protocol.TaskSucceeded:
		if item.Message != "" {
			return item.Name + item.Message
		}
		return "已下载：" + item.Name
	case protocol.TaskCanceled:
		return "已取消：" + item.Name
	default:
		if item.Message == "" {
			return item.Name + "下载失败"
		}
		return item.Name + "下载失败：" + item.Message
	}
}

// downloadDir is where downloads go: the directory the listener chose, or
// the system's downloads directory when they chose none.
func (a *app) downloadDir() (string, error) {
	dir := strings.TrimSpace(a.settings.DownloadDir)
	if dir == "" {
		path, err := mygo.App.Path(mygo.PathDownloads)
		if err != nil {
			return "", fmt.Errorf("找不到系统下载目录：%w", err)
		}
		return filepath.Clean(path), nil
	}
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("下载目录不可用：%w", err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), string(os.PathSeparator)))
	}
	return filepath.Clean(dir), nil
}

// writableDir makes the download directory when it is not there, and says
// whether a file may be written in it: a directory the listener cannot
// write to is worth saying before a batch of downloads starts.
func writableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("下载目录不可用：%w", err)
	}
	f, err := os.CreateTemp(dir, ".wagyu-write-*")
	if err != nil {
		return fmt.Errorf("下载目录不可写：%w", err)
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return nil
}

// snapshot is what the settings window and the manager show: the song being
// fetched, the ones waiting their turn, and what finished lately.
func (d *downloads) snapshot() []pluginapi.DownloadItem {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]pluginapi.DownloadItem, 0, 1+len(d.queued)+len(d.recent))
	if d.running != nil {
		out = append(out, d.running.item)
	}
	for _, job := range d.queued {
		item := job.item
		item.Phase = protocol.TaskRunning
		out = append(out, item)
	}
	for _, done := range d.recent {
		out = append(out, done.item)
	}
	return out
}

// optionSnapshot is the last probe answered, with the token it answers.
func (d *downloads) optionSnapshot() ([]pluginapi.DownloadOption, string) {
	if d == nil {
		return nil, ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.optionList, d.token
}

// setOptions publishes the answer to one probe, in the shape the plugin
// reads, once.
func (d *downloads) setOptions(token string, options []downloadOption) {
	list := downloadOptionList(options)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.token, d.optionList = token, list
}

// openBatch starts a batch of downloads that share one desktop task.
func (d *downloads) openBatch() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nextBatch++
	return d.nextBatch
}

// add puts one song in the queue, and counts it in its batch.
func (d *downloads) add(job *downloadJob, task protocol.DesktopTask) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	job.item.ID = d.next
	d.queued = append(d.queued, job)
	batch := job.item.Batch
	d.left[batch]++
	d.task[batch] = task
}

// wakeUp asks the worker to look at the queue.
func (d *downloads) wakeUp() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// take hands over the next song to fetch, if one waits, with a context that
// stops it. Both the context and the way to stop it belong to the job from
// here on, so that the listener can stop it in the same breath.
func (d *downloads) take(parent context.Context) (*downloadJob, context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running != nil || len(d.queued) == 0 {
		return nil, nil, nil
	}
	job := d.queued[0]
	d.queued = d.queued[1:]
	ctx, cancel := context.WithCancel(parent)
	job.cancel = cancel
	d.running = job
	return job, ctx, cancel
}

// release lets the song go and hands the slot to the next one, on the
// thread that fetched it.
func (d *downloads) release(job *downloadJob) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running == job {
		d.running = nil
	}
	job.cancel = nil
}

// queuedOrRunning reports whether a song of this platform is being fetched
// or waiting to be.
func (d *downloads) queuedOrRunning(key downloadRecordKey) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	holds := func(job *downloadJob) bool {
		return job != nil && job.platform == key.platform && player.TrackRef(&job.track) == key.ref
	}
	if holds(d.running) {
		return true
	}
	for _, job := range d.queued {
		if holds(job) {
			return true
		}
	}
	return false
}

// addSkipped puts a song that was not fetched into the list as done, saying
// why: what it was asked for is already there, or already coming.
func (d *downloads) addSkipped(job *downloadJob, task protocol.DesktopTask) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	job.item.ID = d.next
	item := job.item
	item.Phase, item.Message = protocol.TaskSucceeded, job.reason
	job.item = item
	d.recent = append([]finishedDownload{{job: job, item: item}}, d.recent...)
	if len(d.recent) > downloadRecent {
		d.recent = d.recent[:downloadRecent]
	}
	tally := d.tally[item.Batch]
	tally.skipped++
	d.tally[item.Batch] = tally
	d.task[item.Batch] = task
	if d.left[item.Batch] > 0 {
		d.left[item.Batch]--
	}
}

// setProgress moves what is shown of the song being fetched.
func (d *downloads) setProgress(job *downloadJob, done, total int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running != job {
		return
	}
	job.item.Done, job.item.Total = done, total
}

// finish records one song's download among the ones shown, and gives the
// slot to the next.
func (d *downloads) finish(job *downloadJob, item pluginapi.DownloadItem, skipped bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running == job {
		d.running = nil
	}
	job.cancel = nil
	item.ID, item.Batch, item.Name, item.Level = job.item.ID, job.item.Batch, job.item.Name, job.item.Level
	item.Ref = job.item.Ref
	job.item = item
	d.recent = append([]finishedDownload{{job: job, item: item}}, d.recent...)
	if len(d.recent) > downloadRecent {
		d.recent = d.recent[:downloadRecent]
	}
	var tally downloadTally
	switch {
	case item.Phase == protocol.TaskCanceled:
		tally.canceled = 1
	case item.Phase == protocol.TaskFailed:
		tally.failed = 1
	case skipped:
		tally.skipped = 1
	default:
		tally.succeeded = 1
	}
	held := d.tally[job.item.Batch]
	held.succeeded, held.skipped, held.failed, held.canceled =
		held.succeeded+tally.succeeded, held.skipped+tally.skipped,
		held.failed+tally.failed, held.canceled+tally.canceled
	d.tally[job.item.Batch] = held
	if d.left[job.item.Batch] > 0 {
		d.left[job.item.Batch]--
	}
}

// closeDownloadBatch ends the batch when its last song is done and forgets
// what it was keeping about it: it answers how the batch went, worth saying
// when there was more than one song, and whether the batch ended at all.
func (a *app) closeDownloadBatch(batch uint64) (string, bool) {
	d := a.downloads
	d.mu.Lock()
	if d.left[batch] > 0 || d.batched[batch] {
		d.mu.Unlock()
		return "", false
	}
	tally, task := d.tally[batch], d.task[batch]
	delete(d.left, batch)
	delete(d.tally, batch)
	delete(d.task, batch)
	d.batched[batch] = true
	d.mu.Unlock()

	total := tally.total()
	summary := ""
	if total > 1 {
		summary = downloadSummary(tally)
	}
	var err error
	if tally.failed > 0 {
		err = errors.New(downloadSummary(tally))
	}
	a.completeDesktopTask(task, err)
	if total > 1 {
		return summary, true
	}
	return "", true
}

// total is how many songs the batch held.
func (t downloadTally) total() int {
	return t.succeeded + t.skipped + t.failed + t.canceled
}

// downloadSummary says how a batch of downloads went.
func downloadSummary(tally downloadTally) string {
	parts := make([]string, 0, 4)
	for _, count := range []struct {
		n     int
		label string
	}{
		{tally.succeeded, "首已下载"},
		{tally.skipped, "首已存在"},
		{tally.failed, "首失败"},
		{tally.canceled, "首已取消"},
	} {
		if count.n > 0 {
			parts = append(parts, fmt.Sprintf("%d%s", count.n, count.label))
		}
	}
	return strings.Join(parts, "，")
}
