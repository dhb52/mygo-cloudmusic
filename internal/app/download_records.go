package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/storage"
	"github.com/waygu-music-plugin/sdk/protocol"

	"waygu-music/internal/player"
)

// What the listener has kept is remembered by song, so that asking for one
// again says so rather than writing it a second time under a new name. A
// record is only a claim about a file: what the file system says decides.

// downloadRecordKey names one song on one platform, which is what a
// download is of.
type downloadRecordKey struct {
	platform pluginapi.ID
	ref      string
}

// downloadRecord is one song kept in the download directory, with what is
// needed to fetch it again if the listener asks.
type downloadRecord struct {
	Platform pluginapi.ID `json:"platform"`
	Ref      string       `json:"ref"`
	TrackID  int64        `json:"trackId,omitempty"`
	Song     string       `json:"song"`
	Artist   string       `json:"artist,omitempty"`
	Name     string       `json:"name"`
	Level    string       `json:"level"`
	Path     string       `json:"path"`
	Size     int64        `json:"size"`
	At       time.Time    `json:"at"`
}

func (r downloadRecord) key() downloadRecordKey {
	return downloadRecordKey{platform: r.Platform, ref: r.Ref}
}

// track is the song as a platform knows it, enough to fetch it again.
func (r downloadRecord) track() pluginapi.Track {
	track := pluginapi.Track{ID: r.TrackID, Name: r.Song, Ref: r.Ref, Source: string(r.Platform)}
	if r.Artist != "" {
		track.Artists = []pluginapi.Artist{{Name: r.Artist}}
	}
	return track
}

// downloadRecords is what has been kept, and whether each file is still
// where it was put.
type downloadRecords struct {
	mu    sync.Mutex
	path  string
	items map[downloadRecordKey]downloadRecord
	// missing holds the records whose file was not found the last time it
	// was looked for, so that the manager can say so rather than lie.
	missing map[downloadRecordKey]bool
}

func newDownloadRecords() *downloadRecords {
	return &downloadRecords{
		items:   map[downloadRecordKey]downloadRecord{},
		missing: map[downloadRecordKey]bool{},
	}
}

// loadDownloadRecords reads what the listener has kept, where the settings
// are kept, and looks for each file once.
func (a *app) loadDownloadRecords() {
	records := newDownloadRecords()
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
		records.path = filepath.Join(dir, "downloads.json")
		if b, err := os.ReadFile(records.path); err == nil {
			var list []downloadRecord
			if json.Unmarshal(b, &list) == nil {
				for _, record := range list {
					if record.Ref == "" || record.Path == "" {
						continue
					}
					records.items[record.key()] = record
				}
			}
		}
	}
	records.verify()
	a.downloads.records = records
}

// verify looks for every recorded file, so that what is shown is what is
// there.
func (r *downloadRecords) verify() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, record := range r.items {
		if _, err := os.Stat(record.Path); err != nil {
			r.missing[key] = true
			continue
		}
		delete(r.missing, key)
	}
}

// find answers what is kept of one song, and whether its file is there.
func (r *downloadRecords) find(key downloadRecordKey) (downloadRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.items[key]
	if !ok || r.missing[key] {
		return downloadRecord{}, false
	}
	return record, true
}

// remember notes a song just kept, and saves the list.
func (r *downloadRecords) remember(record downloadRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := record.key()
	r.items[key] = record
	delete(r.missing, key)
	r.saveLocked()
}

// forget drops one song's record, file and all being the listener's.
func (r *downloadRecords) forget(key downloadRecordKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[key]; !ok {
		return
	}
	delete(r.items, key)
	delete(r.missing, key)
	r.saveLocked()
}

// clear drops every record, leaving the files where they are.
func (r *downloadRecords) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) == 0 {
		return
	}
	clear(r.items)
	clear(r.missing)
	r.saveLocked()
}

// list is every record, newest first, with whether its file is there.
func (r *downloadRecords) list() []downloadRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]downloadRecord, 0, len(r.items))
	for key, record := range r.items {
		record.Size = r.size(record)
		if r.missing[key] {
			record.Path = ""
			record.Size = 0
		}
		out = append(out, record)
	}
	slices.SortFunc(out, func(a, b downloadRecord) int { return b.At.Compare(a.At) })
	return out
}

// size is what the file takes now, rather than what it took when it came.
func (r *downloadRecords) size(record downloadRecord) int64 {
	if info, err := os.Stat(record.Path); err == nil {
		return info.Size()
	}
	return record.Size
}

// saveLocked writes the records where they are read from.
func (r *downloadRecords) saveLocked() error {
	if r.path == "" {
		return nil
	}
	list := make([]downloadRecord, 0, len(r.items))
	for _, record := range r.items {
		list = append(list, record)
	}
	slices.SortFunc(list, func(a, b downloadRecord) int { return a.At.Compare(b.At) })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return storage.WriteDataFile(r.path, b)
}

// downloadLevelLabel is what the host calls a level in its own windows,
// where no platform is around to say it its way.
func downloadLevelLabel(level string) string {
	switch level {
	case "standard":
		return "标准"
	case "higher":
		return "较高"
	case "exhigh":
		return "极高"
	case "lossless":
		return "无损"
	case "hires":
		return "Hi-Res"
	}
	return level
}

// recordDownload notes what was just kept, which is what a second ask is
// answered from.
func (a *app) recordDownload(job *downloadJob, item pluginapi.DownloadItem) {
	if item.Phase != protocol.TaskSucceeded || item.Path == "" {
		return
	}
	if item.Message == downloadAlreadyRunning {
		return
	}
	size := int64(0)
	if info, err := os.Stat(item.Path); err == nil {
		size = info.Size()
	}
	a.downloads.records.remember(downloadRecord{
		Platform: job.platform,
		Ref:      player.TrackRef(&job.track),
		TrackID:  job.track.ID,
		Song:     job.track.Name,
		Artist:   job.track.ArtistNames(),
		Name:     item.Name,
		Level:    item.Level,
		Path:     item.Path,
		Size:     size,
		At:       time.Now(),
	})
}
