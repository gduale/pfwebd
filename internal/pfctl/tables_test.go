package pfctl

import (
	"context"
	"testing"
)

func TestValidTableName(t *testing.T) {
	valid := []string{"blocklist", "allow_list", "t1", "Block-List", "_x"}
	for _, s := range valid {
		if !ValidTableName(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	invalid := []string{"", "-lead", "has space", "a;b", "x/y", "../etc",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} // 32 chars > 31 max
	for _, s := range invalid {
		if ValidTableName(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestValidAddress(t *testing.T) {
	valid := []string{"192.0.2.1", "198.51.100.0/24", "2001:db8::1", "2001:db8::/32"}
	for _, s := range valid {
		if !ValidAddress(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	invalid := []string{"", "foo", "192.0.2.1; rm -rf /", "192.0.2.999",
		"192.0.2.1/33", "-h", "1.2.3.4 5.6.7.8"}
	for _, s := range invalid {
		if ValidAddress(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestMockTablesRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := NewClient(NewMockRunner())

	tables, err := c.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 || tables[0] != "allowlist" || tables[1] != "blocklist" {
		t.Fatalf("unexpected tables: %v", tables)
	}

	addrs, err := c.TableAddresses(ctx, "blocklist")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 3 {
		t.Fatalf("expected 3 seed addresses, got %v", addrs)
	}

	if err := c.TableAdd(ctx, "blocklist", "203.0.113.99"); err != nil {
		t.Fatal(err)
	}
	addrs, _ = c.TableAddresses(ctx, "blocklist")
	if len(addrs) != 4 {
		t.Fatalf("expected 4 addresses after add, got %v", addrs)
	}

	if err := c.TableDelete(ctx, "blocklist", "203.0.113.99"); err != nil {
		t.Fatal(err)
	}
	addrs, _ = c.TableAddresses(ctx, "blocklist")
	if len(addrs) != 3 {
		t.Fatalf("expected 3 addresses after delete, got %v", addrs)
	}

	if _, err := c.r.RunTable(ctx, TableAdd, "blocklist", "not-an-ip"); err == nil {
		t.Error("expected error for invalid address")
	}
	if _, err := c.r.RunTable(ctx, TableShow, "nope", ""); err == nil {
		t.Error("expected error for unknown table")
	}
}
