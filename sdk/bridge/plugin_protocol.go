package bridge

import (
	"errors"
	"slices"

	"github.com/waygu-music-plugin/sdk/protocol"
)

type PluginPrepared protocol.PrepareReply

func (p *PluginPrepared) SetWindowActions(settings, lyrics bool) {
	p.WindowActions = []string{"activate", "show", "hide", "status"}
	if settings {
		p.WindowActions = append(p.WindowActions, "settings")
	}
	if lyrics {
		p.WindowActions = append(p.WindowActions, protocol.WindowToggleDesktopLyrics, protocol.WindowToggleDesktopLyricsLock)
	}
}

func ValidateWindowActions(p PluginPrepared) error {
	if !p.SupportsWindowAction("activate") || !p.SupportsWindowAction("show") {
		return errors.New("plugin did not declare required window actions")
	}
	return nil
}

func (p PluginPrepared) SupportsWindowAction(action string) bool {
	return slices.Contains(p.WindowActions, action)
}
