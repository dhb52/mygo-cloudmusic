package platformrpc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/waygu-music-plugin/sdk/protocol"
)

var testIdentity = Identity{Package: "fixture", Entry: "fixture"}
var testHost = Offer{Version: protocol.Version, Needs: []string{CapabilityWindow}, Provides: []string{CapabilityPlayback}}
var testPlugin = Offer{Version: protocol.Version, Needs: []string{CapabilityPlayback}, Provides: []string{CapabilityWindow}}

type transportChild struct{ link *ChildLink }

func (c transportChild) Hello(req HelloRequest, reply *HelloReply) error {
	if err := CheckCompatibility(req.Host, testPlugin); err != nil {
		reply.Failure = Failure{Code: "incompatible", Message: err.Error()}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.link.Bind(ctx, req, func() { os.Exit(0) }); err != nil {
		return err
	}
	var response string
	if err := c.link.Call(ctx, "Echo", req.Connection, &response); err != nil {
		return err
	}
	if response != req.Connection {
		return fmt.Errorf("reverse connection did not return the connection ID")
	}
	*reply = HelloReply{Identity: testIdentity, Plugin: testPlugin}
	return nil
}

type transportHost struct{ connection string }

func (h transportHost) Echo(connection string, out *string) error {
	if connection != h.connection {
		return fmt.Errorf("wrong reverse connection ID")
	}
	*out = connection
	return nil
}

func TestMain(m *testing.M) {
	if IsChild() {
		Serve(func(link *ChildLink) any { return transportChild{link: link} })
		return
	}
	os.Exit(m.Run())
}

// A native subprocess accepts the shared version, calls back before preparation and exits on lease loss.
func TestProcessNegotiatesReverseChannelAndLease(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	program := Program{Path: path, SHA256: sha256.Sum256(binary), Identity: testIdentity, StateDir: t.TempDir()}
	host := func(id string) any { return transportHost{connection: id} }
	old := testHost
	old.Version = 0
	_, err = Start(ctx, program, old, host)
	var failure Failure
	if !errors.As(err, &failure) || failure.Code != "incompatible" {
		t.Fatalf("a host without the version field was not rejected: %v", err)
	}
	p, err := Start(ctx, program, testHost, host)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Connection() == "" {
		t.Fatal("handshake did not establish a connection identity")
	}
	p.Close()
	select {
	case <-p.Done():
	case <-ctx.Done():
		t.Fatal("lost connection did not close the process lease")
	}
}
