package platformrpc

import "github.com/waygu-music-plugin/sdk/protocol"

type (
	Offer        = protocol.Offer
	Identity     = protocol.Identity
	HelloRequest = protocol.HelloRequest
	HelloReply   = protocol.HelloReply
	Scope        = protocol.Scope
	Failure      = protocol.Failure
)

const (
	CapabilityWindow       = protocol.CapabilityWindow
	CapabilityMedia        = protocol.CapabilityMedia
	CapabilityPlayback     = protocol.CapabilityPlayback
	CapabilitySettings     = protocol.CapabilitySettings
	CapabilityMediaStreams = protocol.CapabilityMediaStreams
)

func CheckCompatibility(host, extension Offer) error {
	return protocol.CheckCompatibility(host, extension)
}
