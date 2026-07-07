package client

import (
	"context"
	"os"
	"slices"
	"testing"
)

// TestLiveAPI exercises the client against a running pfwebd instance
// (typically started with -mock). It is skipped unless
// PFWEBD_TEST_ENDPOINT is set, e.g.:
//
//	PFWEBD_TOKEN=t pfwebd -mock -addr 127.0.0.1:8090 &
//	PFWEBD_TEST_ENDPOINT=http://127.0.0.1:8090 PFWEBD_TEST_TOKEN=t go test ./internal/client/
func TestLiveAPI(t *testing.T) {
	endpoint := os.Getenv("PFWEBD_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("PFWEBD_TEST_ENDPOINT not set")
	}
	c := New(endpoint, os.Getenv("PFWEBD_TEST_TOKEN"))
	ctx := context.Background()

	// Status
	st, err := c.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !st.Enabled {
		t.Error("expected PF to be enabled in mock mode")
	}

	// Tables round-trip
	const addr = "203.0.113.199"
	if err := c.TableAdd(ctx, "blocklist", addr); err != nil {
		t.Fatalf("TableAdd: %v", err)
	}
	tbl, err := c.GetTable(ctx, "blocklist")
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if !slices.Contains(tbl.Addresses, addr) {
		t.Errorf("expected %s in table after add, got %v", addr, tbl.Addresses)
	}
	if err := c.TableDelete(ctx, "blocklist", addr); err != nil {
		t.Fatalf("TableDelete: %v", err)
	}
	tbl, _ = c.GetTable(ctx, "blocklist")
	if slices.Contains(tbl.Addresses, addr) {
		t.Errorf("expected %s removed, got %v", addr, tbl.Addresses)
	}

	// Anchor apply + confirm cycle
	rules := []string{"block in quick on em0 from 203.0.113.0/24"}
	if _, err := c.ApplyAnchor(ctx, rules); err != nil {
		t.Fatalf("ApplyAnchor: %v", err)
	}
	after, err := c.ConfirmAnchor(ctx)
	if err != nil {
		t.Fatalf("ConfirmAnchor: %v", err)
	}
	if len(after.Active) != 1 || after.Active[0] != rules[0] {
		t.Errorf("unexpected active ruleset: %v", after.Active)
	}

	// Cleanup: flush the anchor
	if _, err := c.ApplyAnchor(ctx, nil); err != nil {
		t.Fatalf("ApplyAnchor(flush): %v", err)
	}
	if _, err := c.ConfirmAnchor(ctx); err != nil {
		t.Fatalf("ConfirmAnchor(flush): %v", err)
	}
}
