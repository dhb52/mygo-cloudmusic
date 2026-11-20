package platformrpc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
)

const endpointName = "platform"

var handshake = plugin.HandshakeConfig{
	ProtocolVersion: 1, MagicCookieKey: "WAYGU_PLATFORM_PLUGIN", MagicCookieValue: "waygu-music",
}

type transport struct {
	plugin.NetRPCUnsupportedPlugin
	server func(*plugin.MuxBroker) any
}

func (t transport) Server(b *plugin.MuxBroker) (any, error) { return t.server(b), nil }
func (transport) Client(b *plugin.MuxBroker, c *rpc.Client) (any, error) {
	return &peer{broker: b, client: c}, nil
}

type peer struct {
	broker *plugin.MuxBroker
	client *rpc.Client
}

type Program struct {
	Path     string
	SHA256   [sha256.Size]byte
	Identity Identity
	// StateDir is an absolute, private directory for the entry's local UI
	// state. Host credentials and environment are kept out of the command.
	StateDir string
	// LegacyCacheDir is the host's previous app cache, for entry data migration.
	LegacyCacheDir string
	// CredentialService selects the host's system keychain namespace. An
	// empty value selects development files. Nil keeps the runner default.
	CredentialService *string
	// ApplicationID associates the child windows with the desktop host.
	ApplicationID string
}

// EnvLegacyCacheDir names the optional old cache directory passed to the runner.
const EnvLegacyCacheDir = "WAYGU_PLATFORM_LEGACY_CACHE_DIR"

// EnvCredentialService names the optional credential service passed to the runner.
const EnvCredentialService = "WAYGU_PLATFORM_CREDENTIAL_SERVICE"

// EnvApplicationID is supplied by the plugin launcher.
const EnvApplicationID = "WAYGU_PLATFORM_APPLICATION_ID"

type Process struct {
	client     *plugin.Client
	remote     *peer
	lease      net.Conn
	done       chan struct{}
	connection string
	provides   []string
}

// Start launches a previously verified executable and establishes the
// reverse RPC and lifetime channels before returning.
func Start(ctx context.Context, program Program, offer Offer, host func(string) any) (*Process, error) {
	if !filepath.IsAbs(program.Path) || !filepath.IsAbs(program.StateDir) ||
		program.Identity.Package == "" || program.Identity.Entry == "" || host == nil {
		return nil, errors.New("invalid platform program")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(program.StateDir, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.Command(program.Path)
	cmd.Dir = program.StateDir
	cmd.Env = append(pluginEnvironment(), "WAYGU_PLATFORM_STATE_DIR="+program.StateDir)
	if filepath.IsAbs(program.LegacyCacheDir) {
		cmd.Env = append(cmd.Env, EnvLegacyCacheDir+"="+program.LegacyCacheDir)
	}
	if program.CredentialService != nil {
		cmd.Env = append(cmd.Env, EnvCredentialService+"="+*program.CredentialService)
	}
	if program.ApplicationID != "" {
		cmd.Env = append(cmd.Env, EnvApplicationID+"="+program.ApplicationID)
	}
	config := &plugin.ClientConfig{
		HandshakeConfig:  handshake,
		Plugins:          plugin.PluginSet{endpointName: transport{}},
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SecureConfig:     &plugin.SecureConfig{Checksum: program.SHA256[:], Hash: sha256.New()},
		AutoMTLS:         true,
		StartTimeout:     5 * time.Second,
		Logger:           hclog.NewNullLogger(),
	}
	if os.Getenv("WAYGU_PLUGIN_DEBUG") == "1" {
		config.Stderr, config.SyncStderr = os.Stderr, os.Stderr
	}
	client := plugin.NewClient(config)
	success := false
	defer func() {
		if !success {
			client.Kill()
		}
	}()
	conn, err := client.Client()
	if err != nil {
		return nil, err
	}
	raw, err := conn.Dispense(endpointName)
	if err != nil {
		return nil, err
	}
	var id [16]byte
	rand.Read(id[:])
	remote := raw.(*peer)
	// The server already reserved stream 1 for Dispense. Application
	// channels are allocated by the host after that reservation.
	remote.broker.NextId()
	req := HelloRequest{
		Identity: program.Identity, Host: offer, Reverse: remote.broker.NextId(),
		Lease: remote.broker.NextId(), Connection: hex.EncodeToString(id[:]),
	}
	reverse := rpc.NewServer()
	if err := reverse.RegisterName("Host", host(req.Connection)); err != nil {
		return nil, err
	}
	p := &Process{client: client, remote: remote, done: make(chan struct{}), connection: req.Connection}
	go func() {
		conn, err := remote.broker.Accept(req.Reverse)
		if err == nil {
			reverse.ServeConn(conn)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var reply HelloReply
	if err := p.Call(ctx, "Hello", req, &reply); err != nil {
		return nil, err
	}
	if err := reply.Failure.Err(); err != nil {
		return nil, err
	}
	if err := CheckCompatibility(offer, reply.Plugin); err != nil {
		return nil, err
	}
	if reply.Identity != req.Identity {
		return nil, errors.New("platform returned an unexpected identity")
	}
	lease, err := dialBroker(ctx, remote.broker, req.Lease, true)
	if err != nil {
		return nil, err
	}
	p.lease = lease
	p.provides = reply.Plugin.Provides
	go func() {
		_, _ = io.Copy(io.Discard, lease)
		close(p.done)
	}()
	success = true
	return p, nil
}

func (p *Process) Call(ctx context.Context, method string, request, reply any) error {
	return rpcCall(ctx, p.remote.client, "Plugin."+method, request, reply)
}

func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Connection() string    { return p.connection }
func (p *Process) Provides(capability string) bool {
	for _, provided := range p.provides {
		if capability == provided {
			return true
		}
	}
	return false
}

// ReserveStream allocates a host-owned broker ID. Start accepting it
// before giving the ID to the plugin.
func (p *Process) ReserveStream() (uint32, func() (net.Conn, error)) {
	id := p.remote.broker.NextId()
	return id, func() (net.Conn, error) { return p.remote.broker.Accept(id) }
}

// Close asks the window process to exit through its lease before enforcing
// a bounded shutdown. It must run off the application's UI thread.
func (p *Process) Close() {
	_ = p.lease.Close()
	timeout := time.NewTimer(1500 * time.Millisecond)
	defer timeout.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	defer p.client.Kill()
	for !p.client.Exited() {
		select {
		case <-timeout.C:
			return
		case <-poll.C:
		}
	}
}

// IsChild checks the go-plugin startup cookie before an app chooses the
// child entry point. Actual authentication is handled by AutoMTLS.
func IsChild() bool { return os.Getenv(handshake.MagicCookieKey) == handshake.MagicCookieValue }

// Serve provides the transport entry point for a child process.
func Serve(endpoint func(*ChildLink) any) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshake,
		Plugins: plugin.PluginSet{endpointName: transport{server: func(b *plugin.MuxBroker) any {
			return endpoint(&ChildLink{broker: b})
		}}},
		Logger: hclog.NewNullLogger(),
	})
}

// ChildLink establishes the reverse control channel and reports lease loss.
type ChildLink struct {
	broker *plugin.MuxBroker
	client *rpc.Client
	bound  bool
	mu     sync.Mutex
}

func (l *ChildLink) Bind(ctx context.Context, req HelloRequest, lost func()) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bound || req.Reverse == 0 || req.Lease == 0 || req.Reverse == req.Lease {
		return errors.New("invalid platform channel binding")
	}
	l.bound = true
	conn, err := dialBroker(ctx, l.broker, req.Reverse, true)
	if err != nil {
		return err
	}
	l.client = rpc.NewClient(conn)
	go func() {
		lease, err := l.broker.Accept(req.Lease)
		if err == nil {
			_, _ = io.Copy(io.Discard, lease)
			_ = lease.Close()
		}
		_ = l.client.Close()
		lost()
	}()
	return nil
}

func (l *ChildLink) Call(ctx context.Context, method string, request, reply any) error {
	l.mu.Lock()
	client := l.client
	l.mu.Unlock()
	if client == nil {
		return errors.New("platform reverse connection is unavailable")
	}
	return rpcCall(ctx, client, "Host."+method, request, reply)
}

func (l *ChildLink) DialStream(ctx context.Context, id uint32) (net.Conn, error) {
	if id == 0 {
		return nil, errors.New("invalid plugin stream ID")
	}
	return dialBroker(ctx, l.broker, id, false)
}

func rpcCall(ctx context.Context, client *rpc.Client, method string, request, reply any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// net/rpc can still decode a late response after the caller returns.
	// Keep its decode target separate until the request succeeds.
	result := reflect.ValueOf(reply)
	if result.Kind() != reflect.Pointer || result.IsNil() {
		return errors.New("platform RPC reply must be a non-nil pointer")
	}
	private := reflect.New(result.Elem().Type())
	pending := make(chan *rpc.Call, 1)
	go client.Go(method, request, private.Interface(), pending)
	select {
	case done := <-pending:
		if done.Error != nil {
			return done.Error
		}
		result.Elem().Set(private.Elem())
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func dialBroker(ctx context.Context, broker *plugin.MuxBroker, id uint32, required bool) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result)
	go func() {
		conn, err := broker.Dial(id)
		select {
		case done <- result{conn, err}:
		case <-ctx.Done():
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()
	select {
	case r := <-done:
		return r.conn, r.err
	case <-ctx.Done():
		// A failed bootstrap makes the whole connection unusable. For data
		// streams the broker times out unaccepted IDs and the dial worker
		// closes any late connection.
		if required {
			_ = broker.Close()
		}
		return nil, fmt.Errorf("bind platform channel: %w", ctx.Err())
	}
}

func pluginEnvironment() []string {
	allowed := []string{
		"PATH", "HOME", "USER", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "TZ",
		"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "XDG_DATA_DIRS",
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME",
		"DBUS_SESSION_BUS_ADDRESS", "GDK_BACKEND", "TMPDIR",
		"SYSTEMROOT", "WINDIR", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP",
	}
	env := make([]string, 0, len(allowed))
	for _, variable := range allowed {
		if val, ok := os.LookupEnv(variable); ok && !strings.ContainsRune(val, 0) {
			env = append(env, variable+"="+val)
		}
	}
	return env
}
