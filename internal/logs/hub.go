// Package logs fans a single pflog stream out to many SSE clients.
package logs

import (
	"context"
	"log"
	"sync"
	"time"
)

// Source produces a stream of log lines (e.g. tcpdump on pflog0).
// The returned channel is closed when the stream ends or ctx is done.
type Source interface {
	StreamLogs(ctx context.Context) (<-chan string, error)
}

const (
	backlogSize = 200
	subBuffer   = 64
	maxBackoff  = 30 * time.Second
)

// Hub consumes one Source and broadcasts every line to all subscribers,
// keeping a small backlog so new clients see recent history.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan string]struct{}
	backlog []string
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan string]struct{})}
}

// Run consumes src and broadcasts lines until ctx is cancelled.
// If the stream ends (e.g. tcpdump died), it restarts with backoff.
func (h *Hub) Run(ctx context.Context, src Source) {
	backoff := time.Second
	for ctx.Err() == nil {
		ch, err := src.StreamLogs(ctx)
		if err != nil {
			log.Printf("logs: cannot start stream: %v (retry in %s)", err, backoff)
		} else {
			for line := range ch {
				h.Broadcast(line)
				backoff = time.Second // healthy stream resets the backoff
			}
			if ctx.Err() == nil {
				log.Printf("logs: stream ended, restart in %s", backoff)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// Broadcast appends a line to the backlog and delivers it to every
// subscriber. Slow subscribers (full buffer) miss the line rather than
// blocking the stream.
func (h *Hub) Broadcast(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.backlog = append(h.backlog, line)
	if len(h.backlog) > backlogSize {
		h.backlog = h.backlog[len(h.backlog)-backlogSize:]
	}
	for ch := range h.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

// Subscribe registers a new client. It returns the live channel, a copy
// of the recent backlog, and a cancel function that MUST be called when
// the client disconnects.
func (h *Hub) Subscribe() (<-chan string, []string, func()) {
	ch := make(chan string, subBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	backlog := append([]string(nil), h.backlog...)
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
	return ch, backlog, cancel
}
