package app

import (
	"testing"

	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// activatedRPC is the host's side of a plugin's calls, reached as a
// platform's process reaches it: a prepared scope and one action at a time.
func activatedRPC(t *testing.T, a *app, connection string) func(string, PluginHostCall) error {
	t.Helper()
	scope := a.pluginActivations.Prepare(connection)
	if !a.pluginActivations.Commit(scope) {
		t.Fatal("could not activate the fixture")
	}
	reverse := pluginReverse{app: a, connection: scope.Connection}
	return func(action string, in PluginHostCall) error {
		in.Scope, in.Action = scope, action
		var reply PluginHostReply
		return reverse.apply(bridge.LocalHostCall(in.Wire()), &reply)
	}
}

// A listener quitting from a plugin's own window, menu or tray quits the
// application: the plugin cannot quit the host itself, so it asks, and the
// host quits through the same path its own menu uses.
func TestQuitActionQuitsTheApplication(t *testing.T) {
	a := newTestApp()
	call := activatedRPC(t, a, "quit-test")
	quitted := 0
	previous := quitApp
	quitApp = func() { quitted++ }
	t.Cleanup(func() { quitApp = previous })

	if err := call(protocol.HostQuit, PluginHostCall{}); err != nil {
		t.Fatalf("quit: %v", err)
	}
	if quitted != 1 {
		t.Fatalf("the application was quit %d times, want once", quitted)
	}
}
