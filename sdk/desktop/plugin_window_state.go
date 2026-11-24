package desktop

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/storage"
)

const windowStateFilename = "window-state.json"

// Each process saves only its own windows, so MyGo's per-process cache
// cannot overwrite the host's window geometry or that of another entry.
type pluginWindowStateStore struct {
	dir   string
	entry pluginapi.ID
	keys  []string
}

func (s pluginWindowStateStore) LoadWindowStates() (map[string]mygo.WindowState, error) {
	states, err := readWindowStates(filepath.Join(s.dir, windowStateFilename))
	if !errors.Is(err, fs.ErrNotExist) {
		return states, err
	}
	profile := filepath.Dir(filepath.Dir(s.dir))
	legacy, err := readWindowStates(filepath.Join(profile, windowStateFilename))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	states = make(map[string]mygo.WindowState, len(s.keys))
	for _, key := range s.keys {
		st, ok := legacy["plugin-"+string(s.entry)+"-"+key]
		if !ok || st.Width <= 0 || st.Height <= 0 {
			st, ok = legacy[key]
		}
		if ok && st.Width > 0 && st.Height > 0 {
			states[key] = st
		}
	}
	return states, nil
}

func (s pluginWindowStateStore) SaveWindowStates(states map[string]mygo.WindowState) error {
	data, err := json.Marshal(states)
	if err != nil {
		return err
	}
	return storage.WriteDataFile(filepath.Join(s.dir, windowStateFilename), data)
}

func readWindowStates(path string) (map[string]mygo.WindowState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var states map[string]mygo.WindowState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, err
	}
	return states, nil
}
