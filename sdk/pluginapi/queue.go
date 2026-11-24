package pluginapi

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"

	"github.com/waygu-music-plugin/sdk/pluginapi/paging"
)

// QueueFeedCursor identifies the next batch and the host's current queue edges.
type QueueFeedCursor struct {
	Page            uint64
	FirstID, LastID int64
}

// QueueFeed calls done on the UI thread. An empty successful batch ends a
// list. A failed batch can be requested again at the same cursor.
type QueueFeed func(context.Context, QueueFeedCursor, func([]Track, error))

var ErrQueueFeedUnavailable = errors.New("queue feed unavailable")

// PlayPagedList starts at the selected item and asks the pager for the
// remaining pages. The cursor advances only after the host accepts a batch.
func PlayPagedList(player PlayerPort, pages *paging.Pager[Track], index int) {
	if player == nil || pages == nil {
		return
	}
	items, at, start := pages.Around(index)
	if len(items) == 0 {
		return
	}
	player.PlayList(slices.Clone(items), at, func(ctx context.Context, cursor QueueFeedCursor, done func([]Track, error)) {
		size := pages.PageSize()
		if start >= pages.Len() || cursor.Page > uint64((pages.Len()-start-1)/size) {
			done(nil, nil)
			return
		}
		next := start + int(cursor.Page)*size
		page := next / size
		pages.When(page, func(ok bool) {
			if ctx.Err() != nil {
				return
			}
			if next >= pages.Len() {
				done(nil, nil)
				return
			}
			if !ok {
				done(nil, ErrQueueFeedUnavailable)
				return
			}
			end := min((page+1)*size, pages.Len())
			done(queuePageTracks(pages, next, end), nil)
		})
	})
}

// ShuffledQueueFeed builds each batch from two pages in the supplied order.
// The seed and cursor let a retry reproduce the same order after a failure.
func ShuffledQueueFeed(pages *paging.Pager[Track], order []int, seed int64) QueueFeed {
	return func(ctx context.Context, cursor QueueFeedCursor, done func([]Track, error)) {
		if cursor.Page >= uint64((len(order)+1)/2) {
			done(nil, nil)
			return
		}
		first := int(cursor.Page) * 2
		want := order[first:min(first+2, len(order))]
		items := make([][]Track, len(want))
		left := len(want)
		var failed bool
		for i, page := range want {
			pages.When(page, func(ok bool) {
				if ctx.Err() != nil {
					return
				}
				if ok {
					size := pages.PageSize()
					items[i] = queuePageTracks(pages, page*size, min((page+1)*size, pages.Len()))
				}
				failed = failed || !ok
				left--
				if left != 0 {
					return
				}
				if failed {
					done(nil, ErrQueueFeedUnavailable)
					return
				}
				out := slices.Concat(items...)
				rng := rand.New(rand.NewPCG(uint64(seed)^(cursor.Page+1)*0x517cc1b727220a95, 0x243f6a8885a308d3^cursor.Page))
				rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
				done(out, nil)
			})
		}
	}
}

func queuePageTracks(pages *paging.Pager[Track], start, end int) []Track {
	var out []Track
	for i := start; i < end; i++ {
		if track := pages.Peek(i); track != nil {
			out = append(out, *track)
		}
	}
	return out
}
