package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

// An entry inherits its old window geometry once and saves changes independently of the host and other entries.
func TestPluginWindowStateMigratesWithoutOverwritingHost(t *testing.T) {
	profile := t.TempDir()
	hostFile := filepath.Join(profile, windowStateFilename)
	main := mygo.WindowState{X: 80, Y: 55, Width: 1100, Height: 730}
	settings := mygo.WindowState{X: -473, Y: 70, Width: 560, Height: 690}
	previousChild := mygo.WindowState{X: 140, Y: 200, Width: 1160, Height: 800, PixelX: 280, PixelWidth: 2320}
	legacy, err := json.Marshal(map[string]mygo.WindowState{
		"main": main, "settings": settings, "plugin-songloft-main": previousChild,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostFile, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	songloft := pluginWindowStateStore{dir: filepath.Join(profile, "plugin-ui", "songloft"), entry: "songloft", keys: []string{"main", "settings"}}
	other := pluginWindowStateStore{dir: filepath.Join(profile, "plugin-ui", "gonic"), entry: "gonic", keys: []string{"main", "settings"}}
	states, err := songloft.LoadWindowStates()
	if err != nil || states["main"] != previousChild || states["settings"] != settings {
		t.Fatalf("Songloft did not inherit its saved and shared windows: %v, %v", states, err)
	}
	moved := previousChild
	moved.X, moved.PixelX = 200, 400
	states["main"] = moved
	if err := songloft.SaveWindowStates(states); err != nil {
		t.Fatal(err)
	}
	reloaded, err := songloft.LoadWindowStates()
	if err != nil || reloaded["main"] != moved || reloaded["settings"] != settings {
		t.Fatalf("Songloft did not keep the moved window: %v, %v", reloaded, err)
	}
	sibling, err := other.LoadWindowStates()
	if err != nil || sibling["main"] != main {
		t.Fatalf("another entry inherited Songloft's position: %v, %v", sibling, err)
	}
	b, err := os.ReadFile(hostFile)
	if err != nil || !bytes.Equal(b, legacy) {
		t.Fatalf("saving a plugin window changed the original host file: %v", err)
	}
}
