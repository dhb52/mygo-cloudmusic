package network

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
)

// MediaEndpoint holds the address and request headers for the same audio.
type MediaEndpoint struct {
	URL    string
	Header http.Header
}

type MediaRenew func(context.Context, MediaEndpoint) (MediaEndpoint, error)

// MediaSource keeps refreshed credentials local to one playback resource.
type MediaSource struct {
	mu        sync.Mutex
	current   MediaEndpoint
	revision  uint64
	header    func() http.Header
	renew     MediaRenew
	userAgent string
}

func NewMediaSource(rawURL string, header func() http.Header, renew MediaRenew, userAgent string) *MediaSource {
	return &MediaSource{current: MediaEndpoint{URL: rawURL}, header: header, renew: renew, userAgent: userAgent}
}

// Open retries an unauthorized request once before returning any audio. The
// caller's Range and other explicit request options are used on both requests.
func (s *MediaSource) Open(ctx context.Context, client *http.Client, method string, options http.Header) (*http.Response, error) {
	s.mu.Lock()
	current, revision := s.current, s.revision
	s.mu.Unlock()
	if revision == 0 && s.header != nil {
		current.Header = s.header()
	}
	response, err := s.open(ctx, client, method, current, options)
	if err != nil || s.renew == nil || (response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden) {
		return response, err
	}
	_ = response.Body.Close()
	current, err = s.refresh(ctx, current, revision)
	if err != nil {
		return nil, err
	}
	return s.open(ctx, client, method, current, options)
}

func (s *MediaSource) refresh(ctx context.Context, rejected MediaEndpoint, revision uint64) (MediaEndpoint, error) {
	if err := ctx.Err(); err != nil {
		return MediaEndpoint{}, err
	}
	s.mu.Lock()
	if s.revision != revision {
		current := s.current
		s.mu.Unlock()
		return current, nil
	}
	s.mu.Unlock()
	fresh, err := s.renew(ctx, rejected)
	if err != nil {
		return MediaEndpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return MediaEndpoint{}, err
	}
	fresh.Header = fresh.Header.Clone()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision == revision {
		s.current = fresh
		s.revision++
	}
	return s.current, nil
}

func (s *MediaSource) open(ctx context.Context, client *http.Client, method string, source MediaEndpoint, options http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, source.URL, nil)
	if err != nil {
		return nil, errors.New("invalid audio source address")
	}
	req.Header = source.Header.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", s.userAgent)
	}
	for key, values := range options {
		req.Header[key] = values
	}
	resp, err := client.Do(req)
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		// The URL can carry a platform access token.
		return nil, requestErr.Err
	}
	return resp, err
}
