//go:build !linux && !windows

package desktop

// SetApplicationIdentity is handled by the host bundle on macOS.
func SetApplicationIdentity(string) error { return nil }
