package app

import (
	"context"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"

	"waygu-music/internal/player/mpv"
)

func (h platformHost) DesktopSettings() pluginapi.DesktopSettings {
	a, s := h.a, &h.a.settings
	state := pluginapi.DesktopSettings{
		Preferences: pluginapi.DesktopPreferences{
			Appearance: s.Appearance, AppIcon: s.AppIcon, CacheMB: s.CacheMB,
			Notch: s.Notch, NotchActivity: s.NotchActivity, NotchVirtual: s.NotchVirtual,
			Smoke: s.Smoke, Fog: s.Fog,
			UseMPV: s.UseMPV, MPVExclusive: s.MPVExclusive, MPVGapless: s.MPVGapless,
			MPVAudioDevice: s.MPVAudioDevice,
			DownloadDir:    s.DownloadDir,
		},
		CacheUsed:                   a.cacheUsed,
		CacheTask:                   a.desktopTasks.latest[protocol.TaskCacheClear],
		NotchAvailable:              a.quickPlayerWindow() != nil,
		Updates:                     a.updates,
		DesktopLyricsColorAvailable: true,
		DesktopLyricsColor:          s.DesktopLyricsColor,
		StatusLyricsAvailable:       statusLyricsAvailable(),
		StatusLyricsEnabled:         s.StatusLyrics && statusLyricsAvailable(),
		TaskbarLyricsFontSize:       taskbarLyricsFontSize(s.TaskbarLyricsFontSize),
		TaskbarLyricsFontFamily:     s.LyricsFontFamily,
		LyricsFontAvailable:         true,
		LyricsFontFamily:            s.LyricsFontFamily,
		DownloadAvailable:           true,
		Downloads:                   a.downloads.snapshot(),
	}
	if dir, err := a.downloadDir(); err == nil {
		state.DownloadDir = dir
	}
	state.DownloadOptions, state.DownloadOptionsToken = a.downloads.optionSnapshot()
	if a.updates {
		state.UpdateChecks, state.UpdateDownloads = updateChecks(), updateDownloads()
		state.LastUpdateCheck = lastUpdateCheck()
	}
	return state
}

func (h platformHost) SetDesktopLyricsColor(color string) {
	if h.canWrite() {
		if err := h.a.setDesktopLyricsColor(color); err != nil {
			h.Toast(err.Error())
		}
	}
}

func (h platformHost) SetStatusLyrics(enabled bool) {
	if h.canWrite() {
		if err := h.a.setStatusLyrics(enabled); err != nil {
			h.Toast(err.Error())
		}
	}
}

func (h platformHost) SetTaskbarLyricsFontSize(size int) {
	if h.canWrite() {
		if err := h.a.setTaskbarLyricsFontSize(size); err != nil {
			h.Toast(err.Error())
		}
	}
}

func (h platformHost) SetTaskbarLyricsFontFamily(family string) {
	h.SetLyricsFontFamily(family)
}

func (h platformHost) SetLyricsFontFamily(family string) {
	if h.canWrite() {
		if err := h.a.setLyricsFontFamily(family); err != nil {
			h.Toast(err.Error())
		}
	}
}

func (a *app) quickPlayerPreferences() pluginapi.QuickPlayerPreferences {
	s := &a.settings
	return pluginapi.QuickPlayerPreferences{
		Enabled: s.Notch, Activity: s.NotchActivity,
		Virtual: s.NotchVirtual, Greeted: s.NotchGreeted,
	}
}

func (a *app) saveQuickPlayerPreferences(p pluginapi.QuickPlayerPreferences) {
	s := &a.settings
	s.Notch, s.NotchActivity, s.NotchVirtual, s.NotchGreeted = p.Enabled, p.Activity, p.Virtual, p.Greeted
	_ = s.save()
	if a.settingsWin != nil {
		a.settingsWin.Invalidate()
	}
}

func (h platformHost) ApplyDesktopPreferences(p pluginapi.DesktopPreferences) {
	if !h.canWrite() {
		return
	}
	if err := h.a.applyDesktopPreferences(p); err != nil {
		h.Toast(err.Error())
	}
}

func (a *app) applyDesktopPreferences(p pluginapi.DesktopPreferences) error {
	next := a.settings
	s := &next
	cacheChanged := s.CacheMB != p.CacheMB
	iconChanged := s.AppIcon != p.AppIcon
	appearanceChanged := s.Appearance != p.Appearance
	audioChanged := s.UseMPV != p.UseMPV || s.MPVExclusive != p.MPVExclusive ||
		s.MPVGapless != p.MPVGapless || s.MPVAudioDevice != p.MPVAudioDevice
	s.CacheMB, s.AppIcon, s.Appearance = p.CacheMB, p.AppIcon, p.Appearance
	s.Notch, s.NotchActivity, s.NotchVirtual = p.Notch, p.NotchActivity, p.NotchVirtual
	s.Smoke, s.Fog = p.Smoke, p.Fog
	s.UseMPV, s.MPVExclusive, s.MPVGapless = p.UseMPV, p.MPVExclusive, p.MPVGapless
	s.MPVAudioDevice = p.MPVAudioDevice
	s.DownloadDir = p.DownloadDir
	if err := s.save(); err != nil {
		return err
	}
	a.settings = next
	if appearanceChanged {
		s.applyAppearance()
	}
	if cacheChanged {
		a.applyCacheLimit()
	}
	if iconChanged {
		a.changeAppIcon()
	}
	if audioChanged {
		a.syncMPVSettings()
	}
	a.desktopTasksChanged()
	return nil
}

// RequestDownloadOptions answers, with its token, what one song may be
// downloaded as. The platform is asked off the application thread, so that
// the interface is not held up by it.
func (h platformHost) RequestDownloadOptions(track pluginapi.Track, token string) {
	if !h.canWrite() {
		return
	}
	a := h.a
	ctx, guard := h.authorization()
	if ctx == nil {
		ctx = context.Background()
		guard = func() bool { return true }
	}
	go func() {
		options := a.downloadOptions(ctx, &track)
		if !guard() {
			return
		}
		postDesktopTask(a, func() {
			a.downloads.setOptions(token, options)
			a.desktopTasksChanged()
		})
	}()
}

// Download queues the songs the listener chose, at the level they chose.
func (h platformHost) Download(tracks []pluginapi.Track, level string) {
	if !h.canWrite() {
		return
	}
	if err := h.a.startDownloads(tracks, level); err != nil {
		h.Toast(err.Error())
	}
}

// OpenDownloads shows the listener the host's download manager.
func (h platformHost) OpenDownloads() {
	if !h.canWrite() {
		return
	}
	h.a.openDownloadManager()
}

// CancelDownload stops one song's download.
func (h platformHost) CancelDownload(id uint64) {
	if !h.canWrite() {
		return
	}
	from := h.id
	if from == "" {
		from = h.a.platformID
	}
	if err := h.a.cancelDownload(id, from); err != nil {
		h.Toast(err.Error())
	}
}

func (h platformHost) DecoderStatus() pluginapi.DecoderStatus {
	a := h.a
	download, remove := a.desktopTasks.latest[protocol.TaskDecoderDownload], a.desktopTasks.latest[protocol.TaskDecoderDelete]
	status := pluginapi.DecoderStatus{
		Ready: a.mpvReady(), Downloading: download.Running(), Done: a.mpvDone, Total: a.mpvTotal,
		DownloadTask: download, DeleteTask: remove,
	}
	if !status.Downloading {
		status.Downloaded = mpv.IsDownloaded()
	}
	if a.settings.UseMPV && status.Ready && a.pl != nil {
		for _, device := range a.pl.AudioDevices() {
			status.Devices = append(status.Devices, pluginapi.AudioDevice{
				Name: device.Name, Description: device.Description,
			})
		}
	}
	return status
}

func (h platformHost) ClearAudioCache() {
	if !h.canWrite() {
		return
	}
	if _, err := h.a.clearCache(); err != nil {
		h.Toast(err.Error())
	}
}
func (h platformHost) StartDecoderDownload() {
	if !h.canWrite() {
		return
	}
	if _, err := h.a.startMPVDownload(); err != nil {
		h.Toast(err.Error())
	}
}
func (h platformHost) CancelDecoderDownload() {
	if !h.canWrite() {
		return
	}
	_, _ = h.a.cancelMPVDownload(0, protocol.TaskCanceled)
}
func (h platformHost) DeleteDecoder() {
	if !h.canWrite() {
		return
	}
	h.a.deleteMPV()
}
func (h platformHost) SetUpdateChecks(on bool) {
	if !h.canWrite() {
		return
	}
	setUpdateChecks(on)
	h.a.syncPluginUpdateChecks()
}
func (h platformHost) SetUpdateDownloads(on bool) {
	if !h.canWrite() {
		return
	}
	setUpdateDownloads(on)
}
func (h platformHost) CheckForUpdates() {
	if !h.canWrite() {
		return
	}
	checkForUpdates()
}
