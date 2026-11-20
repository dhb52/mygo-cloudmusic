package bridge

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/protocol"
)

type PluginAudioDescription = protocol.PluginAudioDescription

const (
	pcmSeek = 1 + iota
	pcmRead
)

const (
	pcmOK = iota
	pcmEnd
	pcmRevoked
	pcmFailed
)

const maxPCMSamples = 32 << 10

type pcmCommand struct {
	Operation uint8
	Value     int64
}

type pcmReply struct {
	Status uint8
	Count  uint32
}

type pluginAudio struct {
	desc PluginAudioDescription
	conn net.Conn
	mu   sync.Mutex
}

func NewAudio(desc protocol.PluginAudioDescription, conn net.Conn) pluginapi.Audio {
	return &pluginAudio{desc: desc, conn: conn}
}

func (a *pluginAudio) SampleRate() int { return int(a.desc.Rate) }
func (a *pluginAudio) Channels() int   { return int(a.desc.Channels) }
func (a *pluginAudio) Close() error    { return a.conn.Close() }

func (a *pluginAudio) Read(samples []float32) (int, error) {
	channels := a.Channels()
	size := min(len(samples), maxPCMSamples)
	size -= size % channels
	if size == 0 {
		return 0, nil
	}
	return a.call(pcmCommand{Operation: pcmRead, Value: int64(size)}, samples[:size])
}

func (a *pluginAudio) SetPosition(frame int64) error {
	_, err := a.call(pcmCommand{Operation: pcmSeek, Value: frame}, nil)
	return err
}

func (a *pluginAudio) call(command pcmCommand, samples []float32) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return 0, err
	}
	if err := binary.Write(a.conn, binary.LittleEndian, command); err != nil {
		_ = a.Close()
		return 0, err
	}
	var reply pcmReply
	if err := binary.Read(a.conn, binary.LittleEndian, &reply); err != nil {
		_ = a.Close()
		return 0, err
	}
	if uint64(reply.Count) > uint64(len(samples)) || reply.Count%uint32(a.desc.Channels) != 0 {
		_ = a.Close()
		return 0, errors.New("invalid plugin PCM sample count")
	}
	n := int(reply.Count)
	if n > 0 {
		if err := binary.Read(a.conn, binary.LittleEndian, samples[:n]); err != nil {
			_ = a.Close()
			return 0, err
		}
	}
	switch reply.Status {
	case pcmOK:
		return n, nil
	case pcmEnd:
		return n, io.EOF
	case pcmRevoked:
		return n, context.Canceled
	default:
		return n, errors.New("host could not read or seek PCM audio")
	}
}

func ServeAudio(accept func() (net.Conn, error), audio pluginapi.Audio) {
	defer audio.Close()
	conn, err := accept()
	if err != nil {
		return
	}
	defer conn.Close()
	commands := make(chan pcmCommand, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(commands)
		// Waking the decoder on peer close is needed even when there is
		// no response to write yet.
		defer audio.Close()
		for {
			var cmd pcmCommand
			if err := binary.Read(conn, binary.LittleEndian, &cmd); err != nil {
				return
			}
			select {
			case commands <- cmd:
			case <-done:
				return
			default:
				_ = conn.Close()
				return
			}
		}
	}()
	var samples []float32
	for cmd := range commands {
		var n int
		switch {
		case cmd.Operation == pcmSeek && cmd.Value >= 0:
			err = audio.SetPosition(cmd.Value)
		case cmd.Operation == pcmRead && cmd.Value > 0 && cmd.Value <= maxPCMSamples && cmd.Value%int64(audio.Channels()) == 0:
			if cap(samples) < int(cmd.Value) {
				samples = make([]float32, cmd.Value)
			}
			n, err = audio.Read(samples[:cmd.Value])
		default:
			return
		}
		out := pcmReply{Count: uint32(n)}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			out.Status = pcmEnd
		case errors.Is(err, context.Canceled):
			out.Status = pcmRevoked
		default:
			out.Status = pcmFailed
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := binary.Write(conn, binary.LittleEndian, out); err != nil {
			return
		}
		if n > 0 {
			if err := binary.Write(conn, binary.LittleEndian, samples[:n]); err != nil {
				return
			}
		}
		if out.Status == pcmRevoked {
			return
		}
	}
}
