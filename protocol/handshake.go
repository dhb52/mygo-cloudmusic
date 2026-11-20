// Package protocol defines the value-only wire contract for Waygu Music platform processes.
package protocol

import (
	"errors"
	"fmt"
	"slices"
)

const (
	Version uint16 = 1

	CapabilityWindow         = "window"
	CapabilityMedia          = "media"
	CapabilityPlayback       = "playback"
	CapabilitySettings       = "settings"
	CapabilityMediaStreams   = "media-streams"
	CapabilityMediaRenewal   = "media-renewal"
	CapabilityPlaybackEvents = "playback-events"
	CapabilityDesktopLyrics  = "desktop-lyrics"
	CapabilityDesktopTasks   = "desktop-tasks"
	CapabilityDesktopPatches = "desktop-preference-patch"
)

// Offer describes one side's application protocol independently of the transport.
type Offer struct {
	Version         uint16
	Provides, Needs []string
}

type Identity struct {
	Package, Entry string
}

type HelloRequest struct {
	Identity   Identity
	Host       Offer
	Reverse    uint32
	Lease      uint32
	Connection string
}

type HelloReply struct {
	Identity Identity
	Plugin   Offer
	Failure  Failure
}

// Scope belongs to one connection and one activation. A candidate can read
// while preparing. Its scope allows writes after the host commits it.
type Scope struct {
	Connection string
	Activation uint64
}

// FailureMediaMessage carries a playback message ready to show to the listener.
const FailureMediaMessage = "media-message"

type Failure struct {
	Code, Message string
}

func (f Failure) Err() error {
	if f.Code == "" {
		return nil
	}
	return f
}

func (f Failure) Error() string {
	if f.Message == "" {
		return f.Code
	}
	return f.Code + ": " + f.Message
}

func CheckCompatibility(host, extension Offer) error {
	if host.Version != Version || extension.Version != Version {
		return errors.New("unsupported platform protocol version")
	}
	for _, required := range host.Needs {
		if !slices.Contains(extension.Provides, required) {
			return fmt.Errorf("plugin is missing required capability %q", required)
		}
	}
	for _, required := range extension.Needs {
		if !slices.Contains(host.Provides, required) {
			return fmt.Errorf("host is missing required capability %q", required)
		}
	}
	return nil
}
