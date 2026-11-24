package qqmusic

import "github.com/waygu-music-plugin/sdk/pluginapi"

// trackList exposes static or paged results to the local track view.
type trackList struct {
	Total  func() int
	Len    func() int
	At     func(int) *pluginapi.Track
	Retry  func()
	Failed func() bool
}
