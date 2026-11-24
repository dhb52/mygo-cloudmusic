package artwork

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// Once the covers take more than the limit, those asked for least
// recently go, so that a long session does not keep every cover it
// showed; one asked for again stays, however long ago it loaded.
func TestArtCacheDropsLeastRecentCovers(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := NewCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 100
	for _, key := range []string{"first", "second", "third"} {
		c.add(key, &CachedImage{used: now, size: 30})
		now = now.Add(time.Minute)
	}
	c.Get("first", "first", false)
	c.add("fourth", &CachedImage{used: now, size: 30})
	if _, ok := c.art["second"]; ok {
		t.Error("kept the cover asked for least recently over the limit")
	}
	for _, key := range []string{"first", "fourth"} {
		if _, ok := c.art[key]; !ok {
			t.Errorf("dropped %q, asked for last", key)
		}
	}
}

// Covers stay under the limit however long since they showed. They went
// after 5 minutes not asked for: going back to a page after a while on
// others, or to the window after a while away, loaded them again.
func TestArtCacheKeepsCoversUnderLimit(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := NewCache(func(func()) {})
	c.now = func() time.Time { return now }
	page := &CachedImage{used: now, size: 100 << 10}
	c.add("page", page)
	for range 60 {
		now = now.Add(time.Minute)
		c.Get("other", "other", false)
	}
	if c.Get("page", "page", false) != page {
		t.Error("dropped a cover not asked for in an hour, under the limit")
	}
}

// A cover dropped while something still holds it comes back as it was.
// It loaded again: Now Playing's cover went while other pages showed,
// though Now Playing still held it, and flashed in once it opened again.
func TestArtCacheKeepsHeldCovers(t *testing.T) {
	now := time.Unix(1e9, 0)
	c := NewCache(func(func()) {})
	c.now, c.limit = func() time.Time { return now }, 60
	held := &CachedImage{used: now, size: 30}
	c.add("now playing", held)
	for _, key := range []string{"other", "another"} {
		now = now.Add(time.Minute)
		c.add(key, &CachedImage{used: now, size: 30})
	}
	if _, ok := c.art["now playing"]; ok {
		t.Fatal("kept a cover over the limit")
	}
	if got := c.Get("now playing", "now playing", false); got != held {
		t.Error("loaded again a cover something still held")
	}
	runtime.KeepAlive(held)
}

// Configured artwork fetchers handle platform-private cover URLs before decoding.
func TestLoadArtWithPlatformFetcher(t *testing.T) {
	b, err := os.ReadFile("testdata/animated.webp")
	if err != nil {
		t.Fatal(err)
	}
	called := ""
	_, img, _, err := loadArtWith("service://cover/abc", func(url string) ([]byte, error) { called = url; return b, nil })
	if err != nil {
		t.Fatal(err)
	}
	if called != "service://cover/abc" || img.Bounds().Dx() != 24 {
		t.Fatalf("fetch=%q image=%v", called, img.Bounds())
	}
}

// A cover response from the previous platform is discarded after a platform switch.
func TestArtCacheDropsLatePlatformResponse(t *testing.T) {
	b, err := os.ReadFile("testdata/animated.webp")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	posted := make(chan func(), 1)
	generation := uint64(1)
	c := NewCache(func(fn func()) { posted <- fn })
	c.generation = func() uint64 { return generation }
	c.fetch = func() func(string) ([]byte, error) {
		return func(string) ([]byte, error) {
			close(started)
			<-release
			return b, nil
		}
	}
	c.Get("service://cover/old", "service://cover/old", false)
	<-started
	generation = 2
	close(release)
	postedFn := <-posted
	postedFn()
	if _, ok := c.art["service://cover/old"]; ok {
		t.Fatal("late cover response populated the new platform cache")
	}
}
