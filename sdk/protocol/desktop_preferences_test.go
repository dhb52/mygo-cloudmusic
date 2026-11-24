package protocol

import "testing"

// A download directory is just another patchable field: only defined bits are
// accepted, and an unchanged directory selects nothing.
func TestDesktopPreferencePatchDownloadDir(t *testing.T) {
	base := DesktopPreferences{Appearance: "dark", DownloadDir: "/old"}
	next := base
	next.DownloadDir = "/new"

	patch := base.Changes(next)
	if patch.Fields != PrefDownloadDir || !patch.Valid() {
		t.Fatalf("a changed directory selected %#v (valid %v)", patch.Fields, patch.Valid())
	}
	applied := patch.ApplyTo(base)
	if applied.DownloadDir != "/new" || applied.Appearance != "dark" {
		t.Fatalf("writing back the directory kept %#v", applied)
	}

	cleared := base.Changes(DesktopPreferences{Appearance: "dark"})
	if cleared.Fields != PrefDownloadDir {
		t.Fatalf("clearing the directory selected %#v", cleared.Fields)
	}
	if got := cleared.ApplyTo(base); got.DownloadDir != "" {
		t.Fatalf("clearing the directory kept %q", got.DownloadDir)
	}

	if fields := base.Changes(base).Fields; fields != 0 {
		t.Fatalf("an unchanged patch selected %#v", fields)
	}
	if (DesktopPreferencePatch{Fields: PrefDownloadDir << 1}).Valid() {
		t.Fatal("an unknown preference bit was accepted")
	}
}
