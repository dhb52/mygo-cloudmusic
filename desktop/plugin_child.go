package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/platformrpc"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/network"
	"github.com/waygu-music-plugin/sdk/pluginapi/storage"
	"github.com/waygu-music-plugin/sdk/protocol"
)

var childOffer = platformrpc.Offer{
	Version:  protocol.Version,
	Provides: []string{platformrpc.CapabilityWindow, platformrpc.CapabilityMedia, platformrpc.CapabilityMediaStreams, protocol.CapabilityMediaRenewal},
	Needs: []string{platformrpc.CapabilityPlayback, platformrpc.CapabilitySettings, platformrpc.CapabilityMediaStreams,
		protocol.CapabilityPlaybackEvents, protocol.CapabilityDesktopLyrics, protocol.CapabilityDesktopPatches},
}

type pluginChild struct {
	entry     pluginapi.Registration
	link      *platformrpc.ChildLink
	packageID string
	entries   []pluginapi.Registration
	stateDir  string

	// MyGo window and adapter lifecycle state belongs to the UI thread.
	scope           platformrpc.Scope
	life            pluginapi.Lifecycle
	adapter         pluginapi.Adapter
	windows         map[pluginapi.WindowID]pluginapi.Window
	menu            func() *mygo.Menu
	main            *mygo.Window
	aux             map[pluginapi.WindowID]*mygo.Window
	service         *pluginClientServices
	ctx             context.Context
	cancel          context.CancelFunc
	quitting        bool
	active          bool
	tray            bool
	connection      string
	mediaRenewal    bool
	media           bridge.MediaSlots
	initialDeadline *time.Timer
}

// RunPlatformPlugin hosts a platform's own MyGo windows in the plugin
// process and delegates shared playback to the parent process.
func RunPlatformPlugin(entry pluginapi.Registration) error {
	return RunPlatformPackage(string(entry.ID), []pluginapi.Registration{entry})
}

// RunPlatformPackage lets one executable provide multiple stable entries.
func RunPlatformPackage(id string, entries []pluginapi.Registration) error {
	if !platformrpc.IsChild() {
		return errors.New("start this platform from Wagyu Music")
	}
	dir := os.Getenv("WAYGU_PLATFORM_STATE_DIR")
	if !filepath.IsAbs(dir) {
		return errors.New("missing plugin UI state directory")
	}
	// Use the host profile for the existing credential namespace and a
	// separate directory for plugin UI logs and cache.
	mygo.App.SetPath(mygo.PathUserData, filepath.Dir(filepath.Dir(dir)))
	mygo.App.SetPath(mygo.PathLogs, filepath.Join(dir, "logs"))
	mygo.App.SetPath(mygo.PathCache, filepath.Join(dir, "cache"))
	if legacy := os.Getenv(platformrpc.EnvLegacyCacheDir); filepath.IsAbs(legacy) {
		mygo.App.SetPath(storage.PathHostCache, legacy)
	}
	if service, ok := os.LookupEnv(platformrpc.EnvCredentialService); ok {
		storage.UseCredentialService(service)
	}
	child := &pluginChild{packageID: id, entries: entries, stateDir: dir}
	child.initialDeadline = time.AfterFunc(15*time.Second, func() { os.Exit(1) })
	defer child.initialDeadline.Stop()
	mygo.App.WhenReady(func() {
		go platformrpc.Serve(func(link *platformrpc.ChildLink) any {
			child.link = link
			return child
		})
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		child.quitting = true
		if child.service != nil {
			child.service.lyrics.Destroy()
		}
		if child.cancel != nil {
			child.cancel()
		}
		if child.active && child.life.Deactivate != nil {
			child.life.Deactivate(true)
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { child.showMain() })
	err := mygo.App.Run()
	return err
}

func (p *pluginChild) Hello(req platformrpc.HelloRequest, out *platformrpc.HelloReply) error {
	var entry pluginapi.Registration
	for _, candidate := range p.entries {
		if string(candidate.ID) == req.Identity.Entry {
			entry = candidate
			break
		}
	}
	identity := platformrpc.Identity{Package: p.packageID, Entry: string(entry.ID)}
	err := platformrpc.CheckCompatibility(req.Host, childOffer)
	if entry.Start == nil || req.Identity != identity {
		err = errors.New("plugin entry does not match the requested identity")
	}
	if err != nil {
		out.Failure = bridge.Failure("incompatible", err)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.link.Bind(ctx, req, func() {
		mygo.RunOnMain(func() {
			p.quitting = true
			mygo.App.Quit()
		})
	}); err != nil {
		return err
	}
	mygo.RunOnMain(func() {
		p.entry, p.connection = entry, req.Connection
		p.mediaRenewal = slices.Contains(req.Host.Provides, protocol.CapabilityMediaRenewal)
		p.media = bridge.NewMediaSlots()
	})
	*out = platformrpc.HelloReply{Identity: identity, Plugin: childOffer}
	return nil
}

func (p *pluginChild) Prepare(in protocol.PluginPrepare, out *bridge.PluginPrepared) error {
	mygo.RunOnMain(func() {
		if p.main != nil || in.Scope.Connection != p.connection || in.Scope.Activation == 0 {
			out.Failure = platformrpc.Failure{Code: "invalid-prepare"}
			return
		}
		p.scope, p.tray = in.Scope, in.Tray
		p.ctx, p.cancel = context.WithCancel(context.Background())
		p.windows = make(map[pluginapi.WindowID]pluginapi.Window)
		p.aux = make(map[pluginapi.WindowID]*mygo.Window)
		p.service = newPluginClientServices(p, bridge.LocalSnapshot(in.Facade))
		p.adapter = p.entry.Start(pluginapi.Startup{
			Host: p.service, Settings: pluginChildSettings{p.service, in.Settings},
			RegisterWindow: func(id pluginapi.WindowID, win pluginapi.Window) { p.windows[id] = win },
			RegisterMenu: func(id pluginapi.MenuID, menu func() *mygo.Menu) {
				if id == pluginapi.ApplicationMenu {
					p.menu = menu
				}
			},
			RegisterLifecycle: func(l pluginapi.Lifecycle) { p.life = l },
		})
		if p.adapter == nil || p.windows[pluginapi.MainWindow].Root == nil {
			out.Failure = platformrpc.Failure{Code: "missing-window"}
			return
		}
		p.service.art.SetFetcher(func() func(string) ([]byte, error) {
			return func(raw string) ([]byte, error) {
				_, ref, scoped := pluginapi.ArtworkOwner(raw)
				if scoped {
					raw = ref
				}
				if data, handled, err := p.adapter.FetchArtwork(p.ctx, raw, 600); handled || err != nil || scoped {
					return data, err
				}
				return network.FetchContext(p.ctx, raw)
			}
		}, nil)
		ApplyAppearance(in.Appearance)
		var keys []string
		for _, window := range p.windows {
			if key := window.Options.StateKey; key != "" {
				keys = append(keys, key)
			}
		}
		mygo.App.SetWindowStateStore(pluginWindowStateStore{dir: p.stateDir, entry: p.entry.ID, keys: keys})
		p.main = p.window(pluginapi.MainWindow, true)
		p.service.window.Store(p.main)
		p.main.OnFocus(func() {
			if p.active && p.life.MainFocused != nil {
				p.life.MainFocused()
			}
		})
		p.main.OnClose(func(event *mygo.CloseEvent) {
			if p.quitting {
				return
			}
			event.PreventDefault()
			if p.tray {
				p.main.Hide()
			} else {
				p.quitting = true
				mygo.App.Quit()
			}
		})
		out.Configured = p.adapter.Configured()
		if pref := p.adapter.PlaybackMedia().Preferences; pref != nil {
			out.Preferences = protocol.AudioPreferences(pref())
		}
		out.SetWindowActions(p.windows[pluginapi.SettingsWindow].Root != nil, p.windows[pluginapi.DesktopLyricsWindow].Root != nil)
		p.initialDeadline.Stop()
	})
	return nil
}

func (p *pluginChild) Window(in bridge.PluginAction, out *bridge.PluginActionReply) error {
	mygo.RunOnMain(func() {
		if p.main == nil || in.Scope != p.scope || p.quitting {
			out.Failure = platformrpc.Failure{Code: "stale-activation"}
			return
		}
		switch in.Action {
		case "activate":
			if !p.active {
				if in.DesktopLyrics != nil {
					p.service.lyrics.SetPreferences(bridge.LocalDesktopLyricsPrefs(in.DesktopLyrics))
				}
				if p.life.Activate != nil {
					p.life.Activate(p.ctx, pluginChildContinuity{p.service})
				}
				p.active = true
				if p.life.Ready != nil {
					p.life.Ready(pluginapi.Activation{Context: p.ctx, Main: p.main, ColdStart: true})
				}
				p.installMenu()
				go p.watchPlayback()
			}
			p.showMain()
		case "show":
			p.showMain()
		case "hide":
			p.main.Hide()
		case "settings":
			p.service.OpenSettings()
		case protocol.WindowToggleDesktopLyrics, protocol.WindowToggleDesktopLyricsLock:
			if !p.active || p.windows[pluginapi.DesktopLyricsWindow].Root == nil {
				out.Failure = protocol.Failure{Code: "unsupported-window-action"}
				return
			}
			if in.Action == protocol.WindowToggleDesktopLyrics {
				p.service.ToggleDesktopLyrics()
			} else {
				p.service.ToggleDesktopLyricsLock()
			}
			out.Visible = p.service.DesktopLyricsVisible()
			return
		case "status":
		default:
			out.Failure = platformrpc.Failure{Code: "unknown-action"}
		}
		out.Visible = p.main.IsVisible()
	})
	return nil
}

func (p *pluginChild) window(id pluginapi.WindowID, hidden bool) *mygo.Window {
	return mygo.NewWindow(p.windowOptions(id, hidden))
}

func (p *pluginChild) windowOptions(id pluginapi.WindowID, hidden bool) mygo.WindowOptions {
	entry := p.windows[id]
	options := entry.Options
	options.Hidden = hidden
	if id == pluginapi.MainWindow {
		options.Title = p.entry.Name
	}
	options.Content = ui.View(func(c *ui.Context) {
		// Native backends can draw a hidden window during Prepare.
		// Views may require services initialized by Activate.
		if p.active {
			if id == pluginapi.MainWindow {
				c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyP, func() { p.service.action("manage") })
				if p.windows[pluginapi.DesktopLyricsWindow].Root != nil {
					c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyL, p.service.ToggleDesktopLyricsLock)
				}
			}
			entry.Root(c)
		}
	})
	return options
}

func (p *pluginChild) showMain() {
	if !p.active || p.main == nil || p.quitting {
		return
	}
	if p.main.IsMinimized() {
		p.main.Restore()
	}
	p.main.Show()
	p.main.Focus()
	mygo.App.Focus()
}

func (p *pluginChild) installMenu() {
	var items []*mygo.MenuItem
	if p.menu != nil {
		if menu := p.menu(); menu != nil {
			items = menu.Items()
		}
	}
	items = append(items, &mygo.MenuItem{Label: "插件", Submenu: []*mygo.MenuItem{
		{Label: "插件管理…", Click: func(*mygo.MenuItem, *mygo.Window) { p.service.send(bridge.PluginHostCall{Action: "manage"}) }},
	}})
	if p.windows[pluginapi.DesktopLyricsWindow].Root != nil {
		items = append(items, &mygo.MenuItem{Label: "桌面歌词", Submenu: []*mygo.MenuItem{
			{Label: "显示/隐藏桌面歌词", Click: func(*mygo.MenuItem, *mygo.Window) {
				p.service.ToggleDesktopLyrics()
			}},
			{Label: "切换桌面歌词锁定", Click: func(*mygo.MenuItem, *mygo.Window) {
				p.service.ToggleDesktopLyricsLock()
			}},
		}})
	}
	mygo.App.SetMenu(mygo.NewMenu(items))
}

func (p *pluginChild) callHost(ctx context.Context, in bridge.PluginHostCall, out *bridge.PluginHostReply) error {
	var reply protocol.PluginHostReply
	if err := p.link.Call(ctx, "Use", in.Wire(), &reply); err != nil {
		return err
	}
	*out = bridge.LocalHostReply(reply)
	return nil
}

func (p *pluginChild) Media(in protocol.PluginCall, out *protocol.PluginMedia) error {
	var reply bridge.PluginMedia
	p.mediaRequest(bridge.LocalMediaCall(in), &reply)
	*out = reply.Wire()
	return nil
}

func (p *pluginChild) mediaRequest(in bridge.PluginCall, out *bridge.PluginMedia) {
	var adapter pluginapi.Adapter
	var scope context.Context
	var feed *pluginFeed
	var media bridge.MediaSlots
	var mediaRenewal bool
	mygo.RunOnMain(func() {
		if p.active && !p.quitting && p.scope == in.Scope {
			adapter, scope = p.adapter, p.ctx
			feed = p.service.feed(in.Ref)
			media, mediaRenewal = p.media, p.mediaRenewal
		}
	})
	if adapter == nil {
		out.Failure = platformrpc.Failure{Code: "stale-activation"}
		return
	}
	if in.PreviousURL != "" && (!mediaRenewal || (in.Action != "stream" && in.Action != "cast")) {
		out.Failure = platformrpc.Failure{Code: "invalid-source-renewal"}
		return
	}
	bridge.ServeMedia(scope, media.ForAction(in.Action), p.link.DialStream, in, out, func(ctx context.Context) error {
		return p.resolveMedia(ctx, adapter, feed, in, out)
	})
}

func (p *pluginChild) resolveMedia(ctx context.Context, adapter pluginapi.Adapter, feed *pluginFeed, in bridge.PluginCall, out *bridge.PluginMedia) error {
	if in.Action == "feed" {
		return p.resolveFeed(ctx, feed, in.FeedCursor, out)
	}
	if in.Action != "artwork" && !adapter.OwnsTrack(&in.Track) {
		return platformrpc.Failure{Code: "unknown-track"}
	}
	var err error
	m := adapter.PlaybackMedia()
	switch in.Action {
	case "stream":
		if m.Stream != nil {
			var stream *pluginapi.Stream
			stream, err = m.Stream(ctx, &in.Track, in.Level)
			if err == nil {
				out.Stream, err = bridge.DescribeMediaSource(ctx, p.mediaRenewal, in.PreviousURL, stream)
			}
		} else {
			err = pluginapi.ErrUnavailable
		}
	case "cast":
		if m.CastSource != nil {
			var stream *pluginapi.Stream
			var segments pluginapi.SegmentedSource
			stream, segments, err = m.CastSource(ctx, &in.Track, in.Format)
			if segments != nil {
				segments.Close()
				err = pluginapi.ErrUnavailable
			}
			if err == nil {
				out.Stream, err = bridge.DescribeMediaSource(ctx, p.mediaRenewal, in.PreviousURL, stream)
			}
		}
	case "cover":
		if m.Cover != nil {
			out.Cover, err = m.Cover(ctx, &in.Track)
		}
	case "artwork":
		out.Cover, out.Handled, err = adapter.FetchArtwork(ctx, in.Ref, int(in.Size))
	case "lyrics":
		if m.Lyrics != nil {
			out.Original, out.Trans, out.Word, err = m.Lyrics(ctx, &in.Track)
		}
	case "started":
		mygo.RunOnMain(func() {
			if m.Started != nil && p.active && ctx.Err() == nil {
				m.Started(&in.Track)
			}
		})
	default:
		return platformrpc.Failure{Code: "unknown-action"}
	}
	if err != nil && m.ErrorMessage != nil && (in.Action == "stream" || in.Action == "cast") {
		if message := m.ErrorMessage(&in.Track, err); message != "" {
			return platformrpc.Failure{Code: protocol.FailureMediaMessage, Message: message}
		}
	}
	return err
}

func (p *pluginChild) resolveFeed(ctx context.Context, feed *pluginFeed, requested *pluginapi.QueueFeedCursor, out *bridge.PluginMedia) error {
	if feed == nil {
		return platformrpc.Failure{Code: "stale-feed"}
	}
	if requested == nil {
		return platformrpc.Failure{Code: "missing-feed-cursor"}
	}
	type pageResult struct {
		tracks []pluginapi.Track
		err    error
	}
	result := make(chan pageResult, 1)
	mygo.RunOnMain(func() {
		if !p.active || ctx.Err() != nil {
			return
		}
		completed := false
		feed.next(ctx, *requested, func(tracks []pluginapi.Track, err error) {
			if ctx.Err() != nil || completed {
				return
			}
			completed = true
			result <- pageResult{tracks: tracks, err: err}
		})
	})
	select {
	case page := <-result:
		if page.err != nil {
			return platformrpc.Failure{Code: "feed-unavailable"}
		}
		out.Tracks = page.tracks
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type pluginChildSettings struct {
	services *pluginClientServices
	initial  json.RawMessage
}

func (s pluginChildSettings) Load(dst any) error {
	if len(s.initial) == 0 {
		return nil
	}
	return json.Unmarshal(s.initial, dst)
}

func (s pluginChildSettings) Save(src any) error {
	data, err := json.Marshal(src)
	if err == nil {
		s.services.send(bridge.PluginHostCall{Action: "preferences-save", Settings: data})
		// A background connection may finish by saving its credentials.
		// Prompt the local window to redraw just as the in-process store does.
		s.services.Post(func() {})
	}
	return err
}
