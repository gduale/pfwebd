// Package rules manages the PF anchor dedicated to the web UI, with an
// anti-lockout confirmation cycle: every change is applied immediately
// but rolled back automatically unless the user confirms it in time.
// This guarantees you cannot lock yourself out with a bad rule.
package rules

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Applier loads and inspects the dedicated PF anchor.
// Implemented by pfctl.ExecRunner and pfctl.MockRunner.
type Applier interface {
	AnchorValidate(ctx context.Context, rules string) error
	AnchorLoad(ctx context.Context, rules string) error
	AnchorFlush(ctx context.Context) error
	AnchorRules(ctx context.Context) (string, error)
}

// ErrPending is returned when a change is already awaiting confirmation.
var ErrPending = errors.New("a change is already pending confirmation (confirm or cancel it first)")

// ValidationError marks user input errors (mapped to HTTP 400).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Pending describes an applied-but-unconfirmed change.
type Pending struct {
	Rules    []string  `json:"rules"`
	Deadline time.Time `json:"deadline"`
}

// Status is the manager state exposed to the API.
type Status struct {
	Active  []string `json:"active"`
	Pending *Pending `json:"pending"`
}

type pendingState struct {
	rules    []string
	prev     []string
	deadline time.Time
	timer    *time.Timer
}

// Manager owns the UI anchor ruleset and its confirmation lifecycle.
type Manager struct {
	applier Applier
	timeout time.Duration
	file    string // persistence path, "" disables persistence

	mu      sync.Mutex
	active  []string
	pending *pendingState
}

func NewManager(a Applier, timeout time.Duration, file string) *Manager {
	return &Manager{applier: a, timeout: timeout, file: file, active: []string{}}
}

// Rules may only start with these actions; anchors, includes and file
// loads are forbidden inside the UI-managed anchor.
var (
	rulePrefixRe = regexp.MustCompile(`^(pass|block|match)\b`)
	forbiddenRe  = regexp.MustCompile(`\b(include|anchor|load)\b`)
)

// sanitize trims, drops blanks/comments and enforces the rule policy.
func sanitize(input []string) ([]string, error) {
	clean := []string{}
	for i, r := range input {
		r = strings.TrimSpace(r)
		if r == "" || strings.HasPrefix(r, "#") {
			continue
		}
		lower := strings.ToLower(r)
		if !rulePrefixRe.MatchString(lower) {
			return nil, &ValidationError{fmt.Sprintf(
				"line %d: a rule must start with pass, block or match", i+1)}
		}
		if kw := forbiddenRe.FindString(lower); kw != "" {
			return nil, &ValidationError{fmt.Sprintf(
				"line %d: keyword %q is not allowed in the managed anchor", i+1, kw)}
		}
		clean = append(clean, r)
	}
	return clean, nil
}

func joinRules(rules []string) string {
	return strings.Join(rules, "\n") + "\n"
}

// applyToAnchor loads the rules into the anchor (or flushes it when the
// ruleset is empty). Callers must hold m.mu or be otherwise exclusive.
func (m *Manager) applyToAnchor(ctx context.Context, rules []string) error {
	if len(rules) == 0 {
		return m.applier.AnchorFlush(ctx)
	}
	return m.applier.AnchorLoad(ctx, joinRules(rules))
}

// Apply validates the ruleset, loads it into the anchor and starts the
// confirmation countdown. If Confirm is not called before the deadline,
// the previous ruleset is restored automatically.
func (m *Manager) Apply(ctx context.Context, input []string) (Status, error) {
	clean, err := sanitize(input)
	if err != nil {
		return m.Status(), err
	}
	if len(clean) > 0 {
		if err := m.applier.AnchorValidate(ctx, joinRules(clean)); err != nil {
			return m.Status(), &ValidationError{fmt.Sprintf("pfctl rejected the ruleset: %v", err)}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != nil {
		return m.statusLocked(), ErrPending
	}
	if err := m.applyToAnchor(ctx, clean); err != nil {
		return m.statusLocked(), err
	}
	p := &pendingState{
		rules:    clean,
		prev:     m.active,
		deadline: time.Now().Add(m.timeout),
	}
	p.timer = time.AfterFunc(m.timeout, func() { m.expire(p) })
	m.pending = p
	log.Printf("rules: %d rule(s) applied to anchor, awaiting confirmation (timeout %s)",
		len(clean), m.timeout)
	return m.statusLocked(), nil
}

// expire is the rollback path, fired by the timer.
func (m *Manager) expire(p *pendingState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending != p { // already confirmed or cancelled
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := m.applyToAnchor(ctx, p.prev); err != nil {
		log.Printf("rules: ROLLBACK FAILED: %v", err)
	} else {
		log.Printf("rules: change not confirmed in time, rolled back to previous ruleset")
	}
	m.pending = nil
}

// Confirm makes the pending ruleset the active one and persists it.
func (m *Manager) Confirm() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return m.statusLocked(), &ValidationError{"no pending change to confirm"}
	}
	m.pending.timer.Stop()
	m.active = m.pending.rules
	m.pending = nil
	if err := m.persistLocked(); err != nil {
		log.Printf("rules: persist failed: %v", err)
	}
	log.Printf("rules: change confirmed (%d active rule(s))", len(m.active))
	return m.statusLocked(), nil
}

// Cancel rolls back the pending change immediately.
func (m *Manager) Cancel(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return m.statusLocked(), &ValidationError{"no pending change to cancel"}
	}
	m.pending.timer.Stop()
	prev := m.pending.prev
	m.pending = nil
	if err := m.applyToAnchor(ctx, prev); err != nil {
		return m.statusLocked(), fmt.Errorf("rollback failed: %w", err)
	}
	log.Printf("rules: pending change cancelled, rolled back")
	return m.statusLocked(), nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	st := Status{Active: append([]string{}, m.active...)}
	if m.pending != nil {
		st.Pending = &Pending{
			Rules:    append([]string{}, m.pending.rules...),
			Deadline: m.pending.deadline,
		}
	}
	return st
}

// AnchorLive returns the rules actually loaded in the anchor right now.
func (m *Manager) AnchorLive(ctx context.Context) (string, error) {
	return m.applier.AnchorRules(ctx)
}

func (m *Manager) persistLocked() error {
	if m.file == "" {
		return nil
	}
	data := ""
	if len(m.active) > 0 {
		data = joinRules(m.active)
	}
	return os.WriteFile(m.file, []byte(data), 0o600)
}

// LoadPersisted restores and re-applies the last confirmed ruleset,
// e.g. after a daemon restart or reboot when the anchor starts empty.
// Restored rules were confirmed before, so no confirmation cycle here.
func (m *Manager) LoadPersisted(ctx context.Context) error {
	if m.file == "" {
		return nil
	}
	data, err := os.ReadFile(m.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	clean, err := sanitize(strings.Split(string(data), "\n"))
	if err != nil {
		return err
	}
	if len(clean) == 0 {
		return nil
	}
	if err := m.applyToAnchor(ctx, clean); err != nil {
		return err
	}
	m.mu.Lock()
	m.active = clean
	m.mu.Unlock()
	log.Printf("rules: restored %d rule(s) from %s", len(clean), m.file)
	return nil
}
