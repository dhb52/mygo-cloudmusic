package desktop

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/pluginapi"
)

// LyricsWindow owns the native overlay in both the host and platform
// child. Its methods and callbacks run on the window's application thread.
type LyricsWindow struct {
	native                      *mygo.Window
	prefs                       pluginapi.DesktopLyricsPreferences
	pointerInside, ignoresMouse bool
	nativeHover                 bool
	positionDirty               bool
	poll, saveMove              *time.Timer
	offDisplays                 func()
	save                        func(pluginapi.DesktopLyricsPreferences)
}

func (w *LyricsWindow) Configure(prefs pluginapi.DesktopLyricsPreferences, save func(pluginapi.DesktopLyricsPreferences)) {
	w.save = save
	w.SetPreferences(prefs)
}

func (w *LyricsWindow) SetPreferences(prefs pluginapi.DesktopLyricsPreferences) {
	lockChanged := w.prefs.Locked != prefs.Locked
	w.prefs = prefs
	if runtime.GOOS == "linux" && lockChanged && w.native != nil {
		w.updateInput()
		w.trackPointer()
		w.Invalidate()
	}
}

func (w *LyricsWindow) IsOpen() bool { return w.native != nil }

func (w *LyricsWindow) Open(options mygo.WindowOptions, quitting func() bool) {
	options.Hidden = true
	configureLyricsWindow(&options)
	d := desktopLyricsDisplayArea(mygo.Screen.PrimaryDisplay())
	options.X, options.Y = desktopLyricsDefaultPosition(d, options.Width, options.Height)
	var positionChanged bool
	if prefs := &w.prefs; prefs.PositionSet {
		position := desktopLyricsPosition(mygo.Rectangle{
			X: prefs.X, Y: prefs.Y, Width: options.Width, Height: options.Height,
		}, mygo.Screen.Displays())
		options.X, options.Y = position.X, position.Y
		positionChanged = prefs.X != position.X || prefs.Y != position.Y
		prefs.X, prefs.Y = position.X, position.Y
	}
	win := mygo.NewWindow(options)
	w.native = win
	// MyGo uses the material at creation time to enable DirectComposition.
	// Drop the Acrylic backdrop immediately, keeping the composed surface so
	// transparent pixels reveal the desktop instead of a grey material.
	win.SetVibrancy(mygo.VibrancyNone)
	w.ignoresMouse = runtime.GOOS != "linux" || w.prefs.Locked
	win.SetIgnoreMouseEvents(w.ignoresMouse)
	w.nativeHover = setupLyricsWindow(win, func(inside bool) {
		if w.native == win && win.IsVisible() && !w.prefs.Locked {
			w.setPointerInside(inside)
		}
	})
	win.SetVisibleOnAllWorkspaces(true)
	win.OnClose(func(e *mygo.CloseEvent) {
		if !quitting() {
			e.PreventDefault()
			w.Hide()
		}
	})
	win.OnHide(func() {
		if w.native != win {
			return
		}
		w.pointerInside = false
		w.savePosition()
		stopDesktopLyricsTimer(&w.poll)
	})
	win.OnMove(func() {
		if w.native == win && win.IsVisible() {
			w.recordPosition(win.Bounds())
			if w.positionDirty {
				setDesktopLyricsTimer(&w.saveMove, 180*time.Millisecond, w.savePosition)
			}
		}
	})
	w.offDisplays = mygo.Screen.OnDisplaysChanged(func() {
		if w.native != win {
			return
		}
		position := placeDesktopLyricsWindow(win, options.Width, options.Height)
		if w.Visible() || w.prefs.PositionSet {
			w.recordPosition(position)
			w.savePosition()
		}
	})
	if positionChanged {
		w.save(w.prefs)
	}
}

func (w *LyricsWindow) Destroy() {
	win := w.native
	if win == nil {
		return
	}
	if w.Visible() || w.saveMove != nil {
		w.FlushPosition()
	}
	w.native, w.pointerInside, w.nativeHover = nil, false, false
	stopDesktopLyricsTimer(&w.poll)
	w.offDisplays()
	w.offDisplays = nil
	win.Destroy()
}

func placeDesktopLyricsWindow(w *mygo.Window, width, height int) mygo.Rectangle {
	r := w.Bounds()
	if r.Width <= 0 {
		r.Width = width
	}
	if r.Height <= 0 {
		r.Height = height
	}
	position := desktopLyricsPosition(r, mygo.Screen.Displays())
	if position.X != r.X || position.Y != r.Y {
		w.SetBounds(position)
	}
	return position
}

const (
	desktopLyricsMinVisibleWidth  = 100
	desktopLyricsMinVisibleHeight = 50
)

func desktopLyricsDisplayArea(d mygo.Display) mygo.Rectangle {
	if d.WorkArea.Width > 0 && d.WorkArea.Height > 0 {
		return d.WorkArea
	}
	return d.Bounds
}

func desktopLyricsDefaultPosition(workArea mygo.Rectangle, width, height int) (int, int) {
	if workArea.Width <= 0 || workArea.Height <= 0 {
		return 0, 0
	}
	return workArea.X + max(0, (workArea.Width-width)/2),
		workArea.Y + max(0, workArea.Height-height-24)
}

// desktopLyricsPosition keeps a saved window visible on the current displays.
// It preserves valid negative coordinates for displays placed left or above
// the primary display, while recovering positions left by disconnected ones.
func desktopLyricsPosition(r mygo.Rectangle, displays []mygo.Display) mygo.Rectangle {
	best := -1
	bestArea := 0
	for i, d := range displays {
		a := desktopLyricsDisplayArea(d)
		if a.Width <= 0 || a.Height <= 0 {
			continue
		}
		if r.X >= a.X && r.Y >= a.Y && r.X+r.Width <= a.X+a.Width && r.Y+r.Height <= a.Y+a.Height {
			return r
		}
		w := min(a.X+a.Width, r.X+r.Width) - max(a.X, r.X)
		h := min(a.Y+a.Height, r.Y+r.Height) - max(a.Y, r.Y)
		area := max(w, 0) * max(h, 0)
		if area > bestArea {
			best, bestArea = i, area
		}
	}

	if best >= 0 {
		a := desktopLyricsDisplayArea(displays[best])
		visibleW := min(a.X+a.Width, r.X+r.Width) - max(a.X, r.X)
		visibleH := min(a.Y+a.Height, r.Y+r.Height) - max(a.Y, r.Y)
		if visibleW >= min(desktopLyricsMinVisibleWidth, r.Width) && visibleH >= min(desktopLyricsMinVisibleHeight, r.Height) {
			r.X = min(max(r.X, a.X), a.X+max(0, a.Width-r.Width))
			r.Y = min(max(r.Y, a.Y), a.Y+max(0, a.Height-r.Height))
			return r
		}
	}

	// No current display contains enough of the saved window. Prefer the
	// primary display, then the first usable display in case enumeration is
	// temporarily incomplete during a display change.
	for _, d := range displays {
		a := desktopLyricsDisplayArea(d)
		if d.Primary && a.Width > 0 && a.Height > 0 {
			r.X, r.Y = desktopLyricsDefaultPosition(a, r.Width, r.Height)
			return r
		}
	}
	for _, d := range displays {
		a := desktopLyricsDisplayArea(d)
		if a.Width > 0 && a.Height > 0 {
			r.X, r.Y = desktopLyricsDefaultPosition(a, r.Width, r.Height)
			return r
		}
	}
	return r
}

func (w *LyricsWindow) Invalidate() {
	if w.native != nil {
		w.native.Invalidate()
	}
}

func (w *LyricsWindow) Visible() bool { return w.native != nil && w.native.IsVisible() }

func (w *LyricsWindow) Hide() {
	if w.Visible() {
		w.FlushPosition()
		w.native.Hide()
	}
}

func (w *LyricsWindow) Toggle() {
	if w.native == nil {
		return
	}
	if w.Visible() {
		w.Hide()
		return
	}
	if w.prefs.PositionSet {
		w.native.SetPosition(w.prefs.X, w.prefs.Y)
	}
	if runtime.GOOS == "linux" {
		w.updateInput()
	}
	w.native.ShowInactive()
	if runtime.GOOS == "linux" {
		// X11 window managers can discard the old state when the panel is
		// unmapped. Request keep-above again with the window now mapped.
		w.native.SetAlwaysOnTop(true)
	}
	w.native.SetVisibleOnAllWorkspaces(true)
	w.updateInput()
	w.trackPointer()
	w.Invalidate()
}

func (w *LyricsWindow) ToggleLock() {
	w.prefs.Locked = !w.prefs.Locked
	w.savePrefs()
	w.updateInput()
	if runtime.GOOS == "linux" {
		w.trackPointer()
	}
	w.Invalidate()
}

func (w *LyricsWindow) State() pluginapi.DesktopLyricsState {
	return pluginapi.DesktopLyricsState{
		Locked: w.prefs.Locked, PointerInside: w.pointerInside,
		ShowTranslation: w.prefs.ShowTranslation, FontSize: w.prefs.FontSize, Color: w.prefs.Color,
	}
}

func (w *LyricsWindow) SetStyle(translation bool, size float32, color int) {
	w.prefs.ShowTranslation, w.prefs.FontSize, w.prefs.Color = translation, size, color
	w.savePrefs()
	w.Invalidate()
}

func (w *LyricsWindow) FlushPosition() {
	if w.Visible() {
		w.recordPosition(w.native.Bounds())
	}
	w.savePosition()
}

func (w *LyricsWindow) recordPosition(b mygo.Rectangle) {
	if b.Width > 0 && b.Height > 0 && (!w.prefs.PositionSet || w.prefs.X != b.X || w.prefs.Y != b.Y) {
		w.prefs.X, w.prefs.Y, w.prefs.PositionSet = b.X, b.Y, true
		w.positionDirty = true
	}
}

func (w *LyricsWindow) savePosition() {
	if w.positionDirty {
		w.savePrefs()
	}
}

func (w *LyricsWindow) savePrefs() {
	stopDesktopLyricsTimer(&w.saveMove)
	w.positionDirty = false
	w.save(w.prefs)
}

// Poll only when a visible window needs a pointer position the native hover
// events cannot supply. Locked Linux windows never need pointer polling.
func (w *LyricsWindow) trackPointer() {
	if !w.Visible() || w.nativeHover || (runtime.GOOS == "linux" && w.prefs.Locked) {
		stopDesktopLyricsTimer(&w.poll)
		return
	}
	setDesktopLyricsTimer(&w.poll, 33*time.Millisecond, func() {
		if w.Visible() {
			w.updateInput()
			w.trackPointer()
		}
	})
}

func stopDesktopLyricsTimer(slot **time.Timer) {
	if *slot != nil {
		(*slot).Stop()
		*slot = nil
	}
}

func setDesktopLyricsTimer(slot **time.Timer, delay time.Duration, fn func()) {
	stopDesktopLyricsTimer(slot)
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		mygo.RunOnMain(func() {
			if *slot == timer {
				*slot = nil
				fn()
			}
		})
	})
	*slot = timer
}

func (w *LyricsWindow) updateInput() {
	if w.native == nil {
		return
	}
	if runtime.GOOS == "linux" {
		// Input shape follows the lock even while hidden, so GTK can apply
		// it as the surface is mapped again.
		w.setIgnoresMouse(w.prefs.Locked)
		if w.prefs.Locked {
			w.setPointerInside(false)
			return
		}
		if w.nativeHover {
			return
		}
	}
	if !w.Visible() {
		return
	}
	b := w.native.Bounds()
	p := mygo.Screen.CursorScreenPoint()
	inside := p.X >= b.X && p.X < b.X+b.Width && p.Y >= b.Y && p.Y < b.Y+b.Height
	w.setPointerInside(inside)
	if runtime.GOOS != "linux" {
		w.setIgnoresMouse(w.prefs.Locked || !inside)
	}
}

func (w *LyricsWindow) setPointerInside(inside bool) {
	if inside != w.pointerInside {
		w.pointerInside = inside
		w.Invalidate()
	}
}

func (w *LyricsWindow) setIgnoresMouse(ignore bool) {
	if ignore != w.ignoresMouse {
		w.native.SetIgnoreMouseEvents(ignore)
		w.ignoresMouse = ignore
	}
}
