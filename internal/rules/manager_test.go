package rules

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeApplier struct {
	mu           sync.Mutex
	loads        []string
	flushes      int
	failValidate bool
}

func (f *fakeApplier) AnchorValidate(_ context.Context, _ string) error {
	if f.failValidate {
		return errors.New("stdin:1: syntax error")
	}
	return nil
}

func (f *fakeApplier) AnchorLoad(_ context.Context, rules string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, rules)
	return nil
}

func (f *fakeApplier) AnchorFlush(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return nil
}

func (f *fakeApplier) AnchorRules(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.loads) == 0 {
		return "", nil
	}
	return f.loads[len(f.loads)-1], nil
}

func (f *fakeApplier) flushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushes
}

var ctx = context.Background()

func TestApplyThenConfirm(t *testing.T) {
	fa := &fakeApplier{}
	m := NewManager(fa, time.Hour, "")

	st, err := m.Apply(ctx, []string{"block in quick from 203.0.113.0/24", "", "# comment"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Pending == nil || len(st.Pending.Rules) != 1 {
		t.Fatalf("expected 1 pending rule, got %+v", st.Pending)
	}
	if len(st.Active) != 0 {
		t.Errorf("active should still be empty before confirm")
	}

	st, err = m.Confirm()
	if err != nil {
		t.Fatal(err)
	}
	if st.Pending != nil || len(st.Active) != 1 {
		t.Fatalf("expected confirmed state, got %+v", st)
	}
}

func TestApplyRollsBackOnTimeout(t *testing.T) {
	fa := &fakeApplier{}
	m := NewManager(fa, 30*time.Millisecond, "")

	if _, err := m.Apply(ctx, []string{"block in quick from 203.0.113.1"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	st := m.Status()
	if st.Pending != nil {
		t.Error("pending should be cleared after rollback")
	}
	if len(st.Active) != 0 {
		t.Errorf("active should be empty after rollback, got %v", st.Active)
	}
	// Previous ruleset was empty, so the rollback must flush the anchor.
	if fa.flushCount() != 1 {
		t.Errorf("expected 1 flush (rollback), got %d", fa.flushCount())
	}
}

func TestCancelRollsBackImmediately(t *testing.T) {
	fa := &fakeApplier{}
	m := NewManager(fa, time.Hour, "")

	if _, err := m.Apply(ctx, []string{"pass out on em0"}); err != nil {
		t.Fatal(err)
	}
	st, err := m.Cancel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Pending != nil || len(st.Active) != 0 {
		t.Fatalf("expected rollback state, got %+v", st)
	}
	if fa.flushCount() != 1 {
		t.Errorf("expected 1 flush, got %d", fa.flushCount())
	}
}

func TestApplyWhilePendingFails(t *testing.T) {
	m := NewManager(&fakeApplier{}, time.Hour, "")
	if _, err := m.Apply(ctx, []string{"pass out"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Apply(ctx, []string{"block in"}); !errors.Is(err, ErrPending) {
		t.Fatalf("expected ErrPending, got %v", err)
	}
}

func TestSanitizeRejections(t *testing.T) {
	m := NewManager(&fakeApplier{}, time.Hour, "")
	cases := []string{
		"nat on em0 from any to any",                  // forbidden prefix
		"pass in quick anchor \"evil\"",               // anchor keyword
		"block include \"/etc/passwd\"",               // include keyword
		"pass quick load anchor x from \"/tmp/evil\"", // load keyword
	}
	for _, rule := range cases {
		var ve *ValidationError
		if _, err := m.Apply(ctx, []string{rule}); !errors.As(err, &ve) {
			t.Errorf("expected ValidationError for %q, got %v", rule, err)
		}
	}
	// "load" as a substring of a word must NOT be rejected.
	if _, err := m.Apply(ctx, []string{`pass out proto tcp to any port 80 label "downloads"`}); err != nil {
		t.Errorf("label \"downloads\" should be accepted, got %v", err)
	}
}

func TestPfctlValidationFailure(t *testing.T) {
	m := NewManager(&fakeApplier{failValidate: true}, time.Hour, "")
	var ve *ValidationError
	if _, err := m.Apply(ctx, []string{"pass out"}); !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rules.conf")

	m1 := NewManager(&fakeApplier{}, time.Hour, file)
	if _, err := m1.Apply(ctx, []string{"block in quick from 203.0.113.0/24"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m1.Confirm(); err != nil {
		t.Fatal(err)
	}

	fa2 := &fakeApplier{}
	m2 := NewManager(fa2, time.Hour, file)
	if err := m2.LoadPersisted(ctx); err != nil {
		t.Fatal(err)
	}
	st := m2.Status()
	if len(st.Active) != 1 || st.Active[0] != "block in quick from 203.0.113.0/24" {
		t.Fatalf("unexpected restored state: %+v", st)
	}
	if len(fa2.loads) != 1 {
		t.Errorf("expected restored ruleset to be re-applied, got %d loads", len(fa2.loads))
	}
}
