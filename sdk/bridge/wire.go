package bridge

import (
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/eq"
	"github.com/waygu-music-plugin/sdk/pluginapi/lyrics"
	"github.com/waygu-music-plugin/sdk/protocol"
)

func mapWireValues[T, U any](values []T, convert func(T) U) []U {
	if values == nil {
		return nil
	}
	dest := make([]U, len(values))
	for i, value := range values {
		dest[i] = convert(value)
	}
	return dest
}

func wireInt(value int) int64  { return int64(value) }
func localInt(value int64) int { return int(value) }

func wireTrack(t pluginapi.Track) protocol.Track {
	out := protocol.Track{
		PlatformTrack: protocol.PlatformTrack{
			Source: t.Source, Ref: t.Ref, Format: t.Format, Cue: t.Cue, Fav: t.Fav,
			DiscNo: int64(t.DiscNo), TrackNo: int64(t.TrackNo),
		},
		ID: t.ID, Name: t.Name, Album: protocol.Album(t.Album), Duration: t.Duration, Fee: int64(t.Fee),
		Alias: t.Alias, Reason: t.Reason, ProgramID: t.ProgramID, PodcastID: t.PodcastID, Description: t.Description,
		Artists: mapWireValues(t.Artists, func(v pluginapi.Artist) protocol.Artist { return protocol.Artist(v) }),
	}
	if v := t.Privilege; v != nil {
		out.Privilege = &protocol.Privilege{ID: v.ID, St: int64(v.St), Pl: int64(v.Pl)}
	}
	return out
}

func localTrack(t protocol.Track) pluginapi.Track {
	out := pluginapi.Track{
		PlatformTrack: pluginapi.PlatformTrack{
			Source: t.Source, Ref: t.Ref, Format: t.Format, Cue: t.Cue, Fav: t.Fav,
			DiscNo: int(t.DiscNo), TrackNo: int(t.TrackNo),
		},
		ID: t.ID, Name: t.Name, Album: pluginapi.Album(t.Album), Duration: t.Duration, Fee: int(t.Fee),
		Alias: t.Alias, Reason: t.Reason, ProgramID: t.ProgramID, PodcastID: t.PodcastID, Description: t.Description,
		Artists: mapWireValues(t.Artists, func(v protocol.Artist) pluginapi.Artist { return pluginapi.Artist(v) }),
	}
	if v := t.Privilege; v != nil {
		out.Privilege = &pluginapi.Privilege{ID: v.ID, St: int(v.St), Pl: int(v.Pl)}
	}
	return out
}

// A download item carries the song's own reference across, which the
// plugin asked with and matches on.
func wireDownloadItem(v pluginapi.DownloadItem) protocol.DesktopDownloadItem {
	return protocol.DesktopDownloadItem{
		ID: v.ID, Batch: v.Batch, Name: v.Name, Ref: v.Ref, Level: v.Level, Phase: v.Phase,
		Done: v.Done, Total: v.Total, Path: v.Path, Message: v.Message,
	}
}

func localDownloadItem(v protocol.DesktopDownloadItem) pluginapi.DownloadItem {
	return pluginapi.DownloadItem{
		ID: v.ID, Batch: v.Batch, Name: v.Name, Ref: v.Ref, Level: v.Level, Phase: v.Phase,
		Done: v.Done, Total: v.Total, Path: v.Path, Message: v.Message,
	}
}

func wireSession(s *pluginapi.PlaybackSession) *protocol.PlaybackSession {
	if s == nil {
		return nil
	}
	return &protocol.PlaybackSession{
		Queue: mapWireValues(s.Queue, wireTrack), Order: mapWireValues(s.Order, wireInt),
		At: int64(s.At), Shuffle: s.Shuffle, Repeat: protocol.RepeatMode(s.Repeat), Position: s.Position, Radio: s.Radio,
	}
}

func localSession(s *protocol.PlaybackSession) *pluginapi.PlaybackSession {
	if s == nil {
		return nil
	}
	return &pluginapi.PlaybackSession{
		Queue: mapWireValues(s.Queue, localTrack), Order: mapWireValues(s.Order, localInt),
		At: int(s.At), Shuffle: s.Shuffle, Repeat: pluginapi.RepeatMode(s.Repeat), Position: s.Position, Radio: s.Radio,
	}
}

func WireDesktopPreferences(p pluginapi.DesktopPreferences) protocol.DesktopPreferences {
	return protocol.DesktopPreferences{
		Appearance: p.Appearance, AppIcon: p.AppIcon, CacheMB: int64(p.CacheMB), Notch: p.Notch,
		NotchActivity: p.NotchActivity, NotchVirtual: p.NotchVirtual, Smoke: p.Smoke, Fog: p.Fog,
		UseMPV: p.UseMPV, MPVExclusive: p.MPVExclusive, MPVGapless: p.MPVGapless, MPVAudioDevice: p.MPVAudioDevice,
		DownloadDir: p.DownloadDir,
	}
}

func LocalDesktopPreferences(p protocol.DesktopPreferences) pluginapi.DesktopPreferences {
	return pluginapi.DesktopPreferences{
		Appearance: p.Appearance, AppIcon: p.AppIcon, CacheMB: int(p.CacheMB), Notch: p.Notch,
		NotchActivity: p.NotchActivity, NotchVirtual: p.NotchVirtual, Smoke: p.Smoke, Fog: p.Fog,
		UseMPV: p.UseMPV, MPVExclusive: p.MPVExclusive, MPVGapless: p.MPVGapless, MPVAudioDevice: p.MPVAudioDevice,
		DownloadDir: p.DownloadDir,
	}
}

func WireDesktopLyricsPrefs(p pluginapi.DesktopLyricsPreferences) *protocol.DesktopLyricsPreferences {
	return &protocol.DesktopLyricsPreferences{
		Locked: p.Locked, ShowTranslation: p.ShowTranslation, FontSize: p.FontSize, Color: int64(p.Color),
		X: int64(p.X), Y: int64(p.Y), PositionSet: p.PositionSet,
	}
}

func LocalDesktopLyricsPrefs(p *protocol.DesktopLyricsPreferences) pluginapi.DesktopLyricsPreferences {
	return pluginapi.DesktopLyricsPreferences{
		Locked: p.Locked, ShowTranslation: p.ShowTranslation, FontSize: p.FontSize, Color: int(p.Color),
		X: int(p.X), Y: int(p.Y), PositionSet: p.PositionSet,
	}
}

func wireSource(s *PluginSource) *protocol.PluginSource {
	if s == nil {
		return nil
	}
	return &protocol.PluginSource{
		URL: s.URL, Type: s.Type, Level: s.Level, Size: s.Size, Gain: s.Gain, Peak: s.Peak, Headers: s.Headers,
		Digest: (*protocol.ContentDigest)(s.Digest), Renewable: s.Renewable,
	}
}

func localSource(s *protocol.PluginSource) *PluginSource {
	if s == nil {
		return nil
	}
	return &PluginSource{
		URL: s.URL, Type: s.Type, Level: s.Level, Size: s.Size, Gain: s.Gain, Peak: s.Peak, Headers: s.Headers,
		Digest: (*pluginapi.ContentDigest)(s.Digest), Renewable: s.Renewable,
	}
}

func WireSnapshot(s PluginSnapshot) protocol.PluginSnapshot {
	out := protocol.PluginSnapshot{
		Queue: mapWireValues(s.Queue, wireTrack), Order: mapWireValues(s.Order, wireInt),
		QueueRevision: s.QueueRevision, Attempt: s.Attempt, UnchangedQueue: s.UnchangedQueue,
		At: int64(s.At), Playing: s.Playing, Loading: s.Loading, Ready: s.Ready, Shuffle: s.Shuffle,
		Repeat: protocol.RepeatMode(s.Repeat), Radio: s.Radio, Error: s.Error, Position: s.Position, Duration: s.Duration,
		Downloaded: s.Downloaded, DownloadKnown: s.DownloadKnown, LyricsLoaded: s.LyricsLoaded,
		Levels: s.Levels, LevelsLive: s.LevelsLive, Stream: wireSource(s.Stream), Volume: s.Volume,
		Presentation: protocol.Presentation(s.Presentation), EQ: protocol.EQSettings(s.EQ),
		EQAvailable: s.EQAvailable, Search: s.Search,
		Desktop: protocol.DesktopSettings{
			Preferences: WireDesktopPreferences(s.Desktop.Preferences), CacheUsed: s.Desktop.CacheUsed,
			NotchAvailable: s.Desktop.NotchAvailable, Updates: s.Desktop.Updates, UpdateChecks: s.Desktop.UpdateChecks,
			UpdateDownloads: s.Desktop.UpdateDownloads, LastUpdateCheck: s.Desktop.LastUpdateCheck,
			CacheTask:                   s.Desktop.CacheTask,
			DesktopLyricsColorAvailable: s.Desktop.DesktopLyricsColorAvailable,
			DesktopLyricsColor:          s.Desktop.DesktopLyricsColor,
			StatusLyricsAvailable:       s.Desktop.StatusLyricsAvailable,
			StatusLyricsEnabled:         s.Desktop.StatusLyricsEnabled,
			TaskbarLyricsFontSize:       int32(s.Desktop.TaskbarLyricsFontSize),
			TaskbarLyricsFontFamily:     s.Desktop.TaskbarLyricsFontFamily,
			LyricsFontAvailable:         s.Desktop.LyricsFontAvailable,
			LyricsFontFamily:            s.Desktop.LyricsFontFamily,
			DownloadAvailable:           s.Desktop.DownloadAvailable,
			DownloadDir:                 s.Desktop.DownloadDir,
			Downloads:                   mapWireValues(s.Desktop.Downloads, wireDownloadItem),
		},
		Decoder: protocol.DecoderStatus{
			Ready: s.Decoder.Ready, Downloaded: s.Decoder.Downloaded, Downloading: s.Decoder.Downloading,
			SystemPath: s.Decoder.SystemPath, Done: s.Decoder.Done, Total: s.Decoder.Total,
			DownloadTask: s.Decoder.DownloadTask, DeleteTask: s.Decoder.DeleteTask,
			Devices: mapWireValues(s.Decoder.Devices, func(v pluginapi.AudioDevice) protocol.AudioDevice { return protocol.AudioDevice(v) }),
		},
		Casting: protocol.CastingState{
			Available: s.Casting.Available, Sending: s.Casting.Sending, Scanning: s.Casting.Scanning,
			ScanTask: s.Casting.ScanTask, OutputTask: s.Casting.OutputTask,
			Active:  (*protocol.CastingDevice)(s.Casting.Active),
			Devices: mapWireValues(s.Casting.Devices, func(v pluginapi.CastingDevice) protocol.CastingDevice { return protocol.CastingDevice(v) }),
		},
		Lyrics: mapWireValues(s.Lyrics, func(line lyrics.Line) protocol.LyricLine {
			return protocol.LyricLine{At: line.At, End: line.End, Text: line.Text, Translation: line.Translation, Timed: line.Timed,
				Words: mapWireValues(line.Words, func(v lyrics.Word) protocol.LyricWord { return protocol.LyricWord(v) })}
		}),
	}
	if s.Current != nil {
		t := wireTrack(*s.Current)
		out.Current = &t
	}
	return out
}

func LocalSnapshot(s protocol.PluginSnapshot) PluginSnapshot {
	out := PluginSnapshot{
		Queue: mapWireValues(s.Queue, localTrack), Order: mapWireValues(s.Order, localInt),
		QueueRevision: s.QueueRevision, Attempt: s.Attempt, UnchangedQueue: s.UnchangedQueue,
		At: int(s.At), Playing: s.Playing, Loading: s.Loading, Ready: s.Ready, Shuffle: s.Shuffle,
		Repeat: pluginapi.RepeatMode(s.Repeat), Radio: s.Radio, Error: s.Error, Position: s.Position, Duration: s.Duration,
		Downloaded: s.Downloaded, DownloadKnown: s.DownloadKnown, LyricsLoaded: s.LyricsLoaded,
		Levels: s.Levels, LevelsLive: s.LevelsLive, Stream: localSource(s.Stream), Volume: s.Volume,
		Presentation: pluginapi.Presentation(s.Presentation), EQ: eq.Settings(s.EQ),
		EQAvailable: s.EQAvailable, Search: s.Search,
		Desktop: pluginapi.DesktopSettings{
			Preferences: LocalDesktopPreferences(s.Desktop.Preferences), CacheUsed: s.Desktop.CacheUsed,
			NotchAvailable: s.Desktop.NotchAvailable, Updates: s.Desktop.Updates, UpdateChecks: s.Desktop.UpdateChecks,
			UpdateDownloads: s.Desktop.UpdateDownloads, LastUpdateCheck: s.Desktop.LastUpdateCheck,
			CacheTask:                   s.Desktop.CacheTask,
			DesktopLyricsColorAvailable: s.Desktop.DesktopLyricsColorAvailable,
			DesktopLyricsColor:          s.Desktop.DesktopLyricsColor,
			StatusLyricsAvailable:       s.Desktop.StatusLyricsAvailable,
			StatusLyricsEnabled:         s.Desktop.StatusLyricsEnabled,
			TaskbarLyricsFontSize:       int(s.Desktop.TaskbarLyricsFontSize),
			TaskbarLyricsFontFamily:     s.Desktop.TaskbarLyricsFontFamily,
			LyricsFontAvailable:         s.Desktop.LyricsFontAvailable,
			LyricsFontFamily:            s.Desktop.LyricsFontFamily,
			DownloadAvailable:           s.Desktop.DownloadAvailable,
			DownloadDir:                 s.Desktop.DownloadDir,
			Downloads:                   mapWireValues(s.Desktop.Downloads, localDownloadItem),
		},
		Decoder: pluginapi.DecoderStatus{
			Ready: s.Decoder.Ready, Downloaded: s.Decoder.Downloaded, Downloading: s.Decoder.Downloading,
			SystemPath: s.Decoder.SystemPath, Done: s.Decoder.Done, Total: s.Decoder.Total,
			DownloadTask: s.Decoder.DownloadTask, DeleteTask: s.Decoder.DeleteTask,
			Devices: mapWireValues(s.Decoder.Devices, func(v protocol.AudioDevice) pluginapi.AudioDevice { return pluginapi.AudioDevice(v) }),
		},
		Casting: pluginapi.CastingState{
			Available: s.Casting.Available, Sending: s.Casting.Sending, Scanning: s.Casting.Scanning,
			ScanTask: s.Casting.ScanTask, OutputTask: s.Casting.OutputTask,
			Active:  (*pluginapi.CastingDevice)(s.Casting.Active),
			Devices: mapWireValues(s.Casting.Devices, func(v protocol.CastingDevice) pluginapi.CastingDevice { return pluginapi.CastingDevice(v) }),
		},
		Lyrics: mapWireValues(s.Lyrics, func(line protocol.LyricLine) lyrics.Line {
			return lyrics.Line{At: line.At, End: line.End, Text: line.Text, Translation: line.Translation, Timed: line.Timed,
				Words: mapWireValues(line.Words, func(v protocol.LyricWord) lyrics.Word { return lyrics.Word(v) })}
		}),
	}
	if s.Current != nil {
		t := localTrack(*s.Current)
		out.Current = &t
	}
	return out
}

func (in PluginPrepare) Wire() protocol.PluginPrepare {
	return protocol.PluginPrepare{Scope: in.Scope, Facade: WireSnapshot(in.Facade), Settings: in.Settings, Tray: in.Tray, Appearance: in.Appearance, WindowIcon: in.WindowIcon}
}

func (in PluginCall) Wire() protocol.PluginCall {
	return protocol.PluginCall{Scope: in.Scope, Action: in.Action, Track: wireTrack(in.Track), Level: in.Level,
		Format: in.Format, Ref: in.Ref, Size: in.Size, DeadlineNano: in.DeadlineNano, MediaStream: in.MediaStream,
		PreviousURL: in.PreviousURL, FeedCursor: (*protocol.QueueFeedCursor)(in.FeedCursor)}
}

func LocalMediaCall(in protocol.PluginCall) PluginCall {
	return PluginCall{Scope: in.Scope, Action: in.Action, Track: localTrack(in.Track), Level: in.Level,
		Format: in.Format, Ref: in.Ref, Size: in.Size, DeadlineNano: in.DeadlineNano, MediaStream: in.MediaStream,
		PreviousURL: in.PreviousURL, FeedCursor: (*pluginapi.QueueFeedCursor)(in.FeedCursor)}
}

func (r PluginMedia) Wire() protocol.PluginMedia {
	return protocol.PluginMedia{Failure: r.Failure, Stream: wireSource(r.Stream), Original: r.Original,
		Trans: r.Trans, Word: r.Word, CoverSize: r.CoverSize, Handled: r.Handled,
		Tracks: mapWireValues(r.Tracks, wireTrack)}
}

func LocalMedia(r protocol.PluginMedia) PluginMedia {
	return PluginMedia{Failure: r.Failure, Stream: localSource(r.Stream), Original: r.Original,
		Trans: r.Trans, Word: r.Word, CoverSize: r.CoverSize, Handled: r.Handled,
		Tracks: mapWireValues(r.Tracks, localTrack)}
}

func (in PluginHostCall) Wire() protocol.PluginHostCall {
	return protocol.PluginHostCall{Scope: in.Scope, Action: in.Action, Tracks: mapWireValues(in.Tracks, wireTrack),
		Index: int64(in.Index), Position: in.Position, Number: in.Number, Flag: in.Flag, Text: in.Text, Level: in.Level, Feed: in.Feed,
		Session: wireSession(in.Session), Settings: in.Settings, DeadlineNano: in.DeadlineNano,
		QueueRevision: in.QueueRevision, Attempt: in.Attempt, HasRevision: in.HasRevision,
		EQ: protocol.EQSettings(in.EQ), View: protocol.Presentation(in.View),
		DesktopLyrics: in.DesktopLyrics, TaskID: in.TaskID, DesktopPatch: in.DesktopPatch}
}

func LocalHostCall(in protocol.PluginHostCall) PluginHostCall {
	return PluginHostCall{Scope: in.Scope, Action: in.Action, Tracks: mapWireValues(in.Tracks, localTrack),
		Index: int(in.Index), Position: in.Position, Number: in.Number, Flag: in.Flag, Text: in.Text, Level: in.Level, Feed: in.Feed,
		Session: localSession(in.Session), Settings: in.Settings, DeadlineNano: in.DeadlineNano,
		QueueRevision: in.QueueRevision, Attempt: in.Attempt, HasRevision: in.HasRevision,
		EQ: eq.Settings(in.EQ), View: pluginapi.Presentation(in.View),
		DesktopLyrics: in.DesktopLyrics, TaskID: in.TaskID, DesktopPatch: in.DesktopPatch}
}

func (r PluginHostReply) Wire() protocol.PluginHostReply {
	return protocol.PluginHostReply{Failure: r.Failure, Snapshot: WireSnapshot(r.Snapshot), Session: wireSession(r.Session), Audio: r.Audio, Task: r.Task}
}

func LocalHostReply(r protocol.PluginHostReply) PluginHostReply {
	return PluginHostReply{Failure: r.Failure, Snapshot: LocalSnapshot(r.Snapshot), Session: localSession(r.Session), Audio: r.Audio, Task: r.Task}
}
