package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo"
	"github.com/waygu-music-plugin/sdk/bridge"
	"github.com/waygu-music-plugin/sdk/protocol"
)

// childWithHost is a plugin process a host is managing, with a queue for
// what it asks the host.
func childWithHost(connection string, hostQuit bool) *pluginChild {
	child := &pluginChild{connection: connection, hostQuit: hostQuit}
	child.ctx, child.cancel = context.WithCancel(context.Background())
	child.service = &pluginClientServices{
		child:    child,
		commands: make(chan bridge.PluginHostCall, 4),
	}
	return child
}

// A listener quitting from the plugin's own window or menu asks the host to
// quit: the plugin cannot quit the application, so it keeps its own quit
// from happening and waits for the host to end it in turn.
func TestPluginQuitAsksTheHost(t *testing.T) {
	child := childWithHost("connection", true)
	defer child.cancel()
	event := &mygo.QuitEvent{}

	child.beforeQuit(event)

	if !event.DefaultPrevented() {
		t.Fatal("the plugin quit by itself instead of asking the host")
	}
	if child.quitting {
		t.Fatal("the plugin counted itself as quitting before the host said so")
	}
	select {
	case call := <-child.service.commands:
		if call.Action != protocol.HostQuit {
			t.Fatalf("the plugin asked the host for %q, want %q", call.Action, protocol.HostQuit)
		}
	default:
		t.Fatal("the plugin did not ask the host to quit")
	}
}

// A plugin nobody is managing, and one whose host is too old to take the
// request, quits by itself as it always has; so does a session that is
// ending, which cannot be held back.
func TestPluginQuitsByItselfWithoutAHostToAsk(t *testing.T) {
	cases := map[string]struct {
		connection string
		hostQuit   bool
		session    bool
	}{
		"no host":                 {"", true, false},
		"a host that cannot quit": {"connection", false, false},
		"a session ending":        {"connection", true, true},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			child := childWithHost(test.connection, test.hostQuit)
			defer child.cancel()
			event := &mygo.QuitEvent{SessionEnding: test.session}

			child.beforeQuit(event)

			if event.DefaultPrevented() {
				t.Fatal("a quit that should have happened was held back")
			}
			if !child.quitting {
				t.Fatal("the plugin did not count itself as quitting")
			}
			select {
			case call := <-child.service.commands:
				t.Fatalf("the plugin asked the host for %q anyway", call.Action)
			default:
			}
		})
	}
}

// The host quitting the plugin is not the plugin asking the host to quit:
// the request must not come back.
func TestHostDrivenQuitDoesNotAskAgain(t *testing.T) {
	child := childWithHost("connection", true)
	defer child.cancel()
	child.quitting = true

	child.beforeQuit(&mygo.QuitEvent{})

	select {
	case call := <-child.service.commands:
		t.Fatalf("the plugin asked the host for %q while the host was quitting it", call.Action)
	default:
	}
}
