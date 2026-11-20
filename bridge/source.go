package bridge

import (
	"bytes"
	"context"
	"net/http"

	"github.com/waygu-music-plugin/sdk/pluginapi"
	"github.com/waygu-music-plugin/sdk/pluginapi/network"
)

func DescribeMediaSource(ctx context.Context, renewable bool, previousURL string, stream *pluginapi.Stream) (*PluginSource, error) {
	source := DescribeSource(stream)
	if source == nil {
		return nil, nil
	}
	source.Renewable = renewable && source.URL != ""
	if stream.Renew != nil && previousURL != "" && previousURL == source.URL {
		next, err := stream.Renew(ctx, network.MediaEndpoint{URL: source.URL, Header: source.Headers})
		if err != nil {
			return nil, err
		}
		source.URL, source.Headers = next.URL, next.Header
	}
	return source, nil
}

func (s *PluginSource) SameMedia(other *PluginSource) bool {
	if other == nil || other.URL == "" || other.Type != s.Type || other.Level != s.Level {
		return false
	}
	if s.Size > 0 && other.Size > 0 && other.Size != s.Size {
		return false
	}
	return s.Digest == nil || (other.Digest != nil && s.Digest.Algorithm == other.Digest.Algorithm &&
		bytes.Equal(s.Digest.Sum, other.Digest.Sum))
}

func (s *PluginSource) Stream() *pluginapi.Stream {
	if s == nil {
		return nil
	}
	st := &pluginapi.Stream{URL: s.URL, Type: s.Type, Level: s.Level, Size: s.Size, Gain: s.Gain, Peak: s.Peak}
	if len(s.Headers) > 0 {
		st.Header = func() http.Header { return http.Header(s.Headers).Clone() }
	}
	if s.Digest != nil {
		st.Digest = s.Digest
		st.VerifyCachedContent = s.Digest.Matches
	}
	return st
}

func DescribeSource(st *pluginapi.Stream) *PluginSource {
	if st == nil {
		return nil
	}
	s := &PluginSource{URL: st.URL, Type: st.Type, Level: st.Level, Size: st.Size, Gain: st.Gain, Peak: st.Peak, Digest: st.Digest}
	if st.Header != nil {
		s.Headers = st.Header().Clone()
	}
	return s
}
