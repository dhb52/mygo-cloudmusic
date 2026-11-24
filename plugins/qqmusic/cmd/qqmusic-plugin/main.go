package main

import (
	"log"

	"github.com/waygu-music-plugin/sdk/desktop"
	"github.com/waygu-music-plugin/qqmusic"
)

func main() {
	if err := desktop.RunPlatformPlugin(qqmusic.Registration()); err != nil {
		log.Fatal(err)
	}
}
