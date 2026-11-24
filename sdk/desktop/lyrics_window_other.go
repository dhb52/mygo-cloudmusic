//go:build !linux || android || (!amd64 && !arm64)

package desktop

import "github.com/egoist/mygo"

func configureLyricsWindow(*mygo.WindowOptions) {}

func setupLyricsWindow(*mygo.Window, func(bool)) bool { return false }
