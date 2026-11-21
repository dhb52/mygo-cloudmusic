//go:build linux && !android && (amd64 || arm64)

package desktop

import (
	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
)

func configureLyricsWindow(options *mygo.WindowOptions) {
	// A normal frameless GTK window still inherits the application menu.
	options.Panel = true
	options.Frameless = true
	options.Transparent = true
	options.BackgroundColor = "#00000000"
	options.DisableShadow = true
}

// GTK owns the signals on the main thread. Reuse the same callbacks for
// each opening of the lyrics window.
var lyricsHoverCallbacks = make(map[uintptr]func(bool))
var lyricsMappedWindows = make(map[uintptr]*mygo.Window)

var (
	lyricsPointerEnter = purego.NewCallback(func(controller uintptr, _, _ float64, _ uintptr) {
		if moved := lyricsHoverCallbacks[controller]; moved != nil {
			moved(true)
		}
	})
	lyricsPointerLeave = purego.NewCallback(func(controller, _ uintptr) {
		if moved := lyricsHoverCallbacks[controller]; moved != nil {
			moved(false)
		}
	})
	lyricsMapped = purego.NewCallback(func(widget, _, _ uintptr) int32 {
		if win := lyricsMappedWindows[widget]; win != nil && win.IgnoresMouseEvents() {
			// An X11 window manager can frame an already-shaped client after
			// GTK maps it. Reapply the empty input region to notify the frame.
			win.SetIgnoreMouseEvents(true)
		}
		return 0
	})
)

func setupLyricsWindow(win *mygo.Window, moved func(bool)) bool {
	widget := win.NativeHandle()
	if widget == 0 {
		return false
	}
	styleLyricsLayer(widget)
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return false
	}
	defer purego.Dlclose(gtk)
	var connect func(uintptr, string, uintptr, uintptr, uintptr, uint32) uintptr
	if !bindLyricsSymbol(gtk, "g_signal_connect_data", &connect) {
		return false
	}
	lyricsMappedWindows[widget] = win
	connect(widget, "map-event", lyricsMapped, 0, 0, 0)
	win.OnClosed(func() { delete(lyricsMappedWindows, widget) })

	var create func(uintptr) uintptr
	var setPhase func(uintptr, int32)
	var unref func(uintptr)
	// The motion controller is available since GTK 3.24. Older GTK can
	// report hover through polling without changing the native input shape.
	if !bindLyricsSymbol(gtk, "gtk_event_controller_motion_new", &create) ||
		!bindLyricsSymbol(gtk, "gtk_event_controller_set_propagation_phase", &setPhase) ||
		!bindLyricsSymbol(gtk, "g_object_unref", &unref) {
		return false
	}
	controller := create(widget)
	if controller == 0 {
		return false
	}
	const gtkPhaseCapture = 1
	setPhase(controller, gtkPhaseCapture)
	lyricsHoverCallbacks[controller] = moved
	connect(controller, "enter", lyricsPointerEnter, 0, 0, 0)
	connect(controller, "leave", lyricsPointerLeave, 0, 0, 0)
	win.OnClosed(func() {
		delete(lyricsHoverCallbacks, controller)
		unref(controller)
	})
	return true
}

func styleLyricsLayer(widget uintptr) {
	lib, err := purego.Dlopen("libgtk-layer-shell.so.0", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return
	}
	defer purego.Dlclose(lib)
	var isLayerWindow func(uintptr) bool
	var setLayer func(uintptr, int32)
	if !bindLyricsSymbol(lib, "gtk_layer_is_layer_window", &isLayerWindow) ||
		!bindLyricsSymbol(lib, "gtk_layer_set_layer", &setLayer) || !isLayerWindow(widget) {
		return
	}
	// Float above normal windows, leaving popup menus above the lyrics.
	const gtkLayerTop = 2
	setLayer(widget, gtkLayerTop)
}

func bindLyricsSymbol(lib uintptr, name string, fn any) bool {
	symbol, err := purego.Dlsym(lib, name)
	if err != nil {
		return false
	}
	purego.RegisterFunc(fn, symbol)
	return true
}
