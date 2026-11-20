package desktop

import (
	"github.com/egoist/mygo"
	"testing"
)

// Desktop lyrics recover to the primary display when their saved monitor is disconnected.
func TestDesktopLyricsPositionRecoversDisconnectedDisplay(t *testing.T) {
	displays := []mygo.Display{
		{Primary: true, WorkArea: mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1040}},
		{WorkArea: mygo.Rectangle{X: 1920, Y: 0, Width: 2560, Height: 1400}},
	}
	r := desktopLyricsPosition(mygo.Rectangle{X: 2200, Y: 900, Width: 900, Height: 160}, displays[:1])
	if r.X != 510 || r.Y != 856 {
		t.Fatalf("disconnected display position became %+v, want primary bottom center", r)
	}
}

// Desktop lyrics keep valid negative coordinates for a display left of the primary one.
func TestDesktopLyricsPositionKeepsNegativeDisplayCoordinates(t *testing.T) {
	displays := []mygo.Display{
		{Primary: true, WorkArea: mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1040}},
		{WorkArea: mygo.Rectangle{X: -1920, Y: 0, Width: 1920, Height: 1080}},
	}
	want := mygo.Rectangle{X: -1800, Y: 100, Width: 900, Height: 160}
	if got := desktopLyricsPosition(want, displays); got != want {
		t.Fatalf("valid negative display position became %+v, want %+v", got, want)
	}
}

// Desktop lyrics clamp a partially visible saved position back into its display work area.
func TestDesktopLyricsPositionClampsPartialWindow(t *testing.T) {
	displays := []mygo.Display{{Primary: true, WorkArea: mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1040}}}
	r := desktopLyricsPosition(mygo.Rectangle{X: 1800, Y: 900, Width: 900, Height: 160}, displays)
	if r.X != 1020 || r.Y != 880 {
		t.Fatalf("partially visible position became %+v, want clamped position", r)
	}
}
