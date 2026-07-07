package logs

import (
	"fmt"
	"testing"
)

func TestHubBroadcastAndBacklog(t *testing.T) {
	h := NewHub()
	h.Broadcast("line-1")

	ch, backlog, cancel := h.Subscribe()
	defer cancel()

	if len(backlog) != 1 || backlog[0] != "line-1" {
		t.Fatalf("unexpected backlog: %v", backlog)
	}

	h.Broadcast("line-2")
	select {
	case got := <-ch:
		if got != "line-2" {
			t.Errorf("expected line-2, got %q", got)
		}
	default:
		t.Fatal("expected a line on the subscriber channel")
	}
}

func TestHubBacklogTrimming(t *testing.T) {
	h := NewHub()
	for i := 0; i < backlogSize+50; i++ {
		h.Broadcast(fmt.Sprintf("line-%d", i))
	}
	_, backlog, cancel := h.Subscribe()
	defer cancel()

	if len(backlog) != backlogSize {
		t.Fatalf("expected backlog of %d, got %d", backlogSize, len(backlog))
	}
	if backlog[len(backlog)-1] != fmt.Sprintf("line-%d", backlogSize+49) {
		t.Errorf("unexpected last backlog line: %q", backlog[len(backlog)-1])
	}
}

func TestHubUnsubscribe(t *testing.T) {
	h := NewHub()
	ch, _, cancel := h.Subscribe()
	cancel()
	h.Broadcast("after-cancel")
	select {
	case line, ok := <-ch:
		if ok {
			t.Errorf("did not expect line after cancel: %q", line)
		}
	default:
		// channel empty: expected
	}
}

func TestHubSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub()
	_, _, cancel := h.Subscribe()
	defer cancel()
	// Fill well past the subscriber buffer; Broadcast must not block.
	for i := 0; i < subBuffer*3; i++ {
		h.Broadcast("flood")
	}
}
