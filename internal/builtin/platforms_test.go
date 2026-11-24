package builtin

import "testing"

// A fresh host can construct both bundled platforms, Feiniu first, which is
// the one a listener who has not chosen a platform starts on.
func TestBundledPlatforms(t *testing.T) {
	entries := Platforms()
	if len(entries) != 2 || entries[0].ID != "fnmusic" || entries[1].ID != "qqmusic" {
		t.Fatalf("unexpected built-in platform registrations: %+v", entries)
	}
	for _, entry := range entries {
		if entry.Start == nil {
			t.Fatalf("%s cannot be constructed: %+v", entry.ID, entry)
		}
	}
}
