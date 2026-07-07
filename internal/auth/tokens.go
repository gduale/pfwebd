// Package auth manages API tokens for pfwebd: a set of named, scoped
// tokens stored as SHA-256 hashes, loaded from a file that can be
// reloaded at runtime (SIGHUP) without restarting the daemon.
//
// Tokens are never stored or compared in clear text. The file format is
// one token per line:
//
//	# name        scope   sha256(token)
//	terraform     write   9f86d081884c7d659a2feaa0c55ad015a3bf4f1b...
//	monitoring    read    2c26b46b68ffc68ff99b453c1d30413413422d70...
//
// Blank lines and lines starting with '#' are ignored. The scope is
// either "read" or "write" ("write" implies read).
package auth

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Scope is the capability granted by a token.
type Scope string

const (
	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"
)

// tokenPrefix marks pfwebd tokens so they can be recognised (and caught
// by secret scanners) without revealing the secret itself.
const tokenPrefix = "pfw_"

// DefaultTokensFile is the conventional location of the tokens file, matching
// the rc.d default flags. It is used by helpers such as AppendToken when no
// explicit path is given.
const DefaultTokensFile = "/etc/pfwebd/tokens"

type entry struct {
	name  string
	scope Scope
	hash  [32]byte
}

// Store holds the active token set. It is safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	path     string // tokens file; "" disables file-based tokens
	env      *entry // optional PFWEBD_TOKEN, always write scope
	fileToks []entry
}

// New builds a token store. envToken, if non-empty, is registered as an
// implicit write-scoped token named "env" (from PFWEBD_TOKEN). path,
// if non-empty, points to a tokens file loaded immediately and on every
// Reload. A missing file is not an error (it yields no file tokens).
func New(path, envToken string) (*Store, error) {
	s := &Store{path: path}
	if envToken != "" {
		e := entry{name: "env", scope: ScopeWrite, hash: sha256.Sum256([]byte(envToken))}
		s.env = &e
	}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reload re-reads the tokens file. On any parse error the previous token
// set is kept unchanged and the error is returned, so a bad edit never
// locks the caller out mid-flight.
func (s *Store) Reload() error {
	if s.path == "" {
		return nil
	}
	toks, err := parseFile(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.fileToks = toks
	s.mu.Unlock()
	return nil
}

// Lookup returns the name and scope of the token matching presented, and
// whether a match was found. Comparison is constant-time.
func (s *Store) Lookup(presented string) (name string, scope Scope, ok bool) {
	if presented == "" {
		return "", "", false
	}
	h := sha256.Sum256([]byte(presented))
	match := func(e entry) bool {
		return subtle.ConstantTimeCompare(h[:], e.hash[:]) == 1
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.env != nil && match(*s.env) {
		return s.env.name, s.env.scope, true
	}
	for _, e := range s.fileToks {
		if match(e) {
			return e.name, e.scope, true
		}
	}
	return "", "", false
}

// Empty reports whether no token at all is configured (so writes are
// effectively disabled and pfwebd is a pure dashboard).
func (s *Store) Empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.env == nil && len(s.fileToks) == 0
}

// Count returns the number of configured tokens (env token included).
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.fileToks)
	if s.env != nil {
		n++
	}
	return n
}

func parseFile(path string) ([]entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil // no file yet: simply no file-based tokens
		}
		return nil, err
	}
	defer f.Close()

	var out []entry
	seen := make(map[string]bool)
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: expected 'name scope sha256', got %d field(s)", path, line, len(fields))
		}
		name, scopeStr, hashStr := fields[0], fields[1], fields[2]

		var scope Scope
		switch Scope(scopeStr) {
		case ScopeRead:
			scope = ScopeRead
		case ScopeWrite:
			scope = ScopeWrite
		default:
			return nil, fmt.Errorf("%s:%d: invalid scope %q (use \"read\" or \"write\")", path, line, scopeStr)
		}

		raw, err := hex.DecodeString(hashStr)
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("%s:%d: invalid sha256 hash %q (want 64 hex chars)", path, line, hashStr)
		}
		if seen[name] {
			return nil, fmt.Errorf("%s:%d: duplicate token name %q", path, line, name)
		}
		seen[name] = true

		var h [32]byte
		copy(h[:], raw)
		out = append(out, entry{name: name, scope: scope, hash: h})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Hash returns the lowercase hex SHA-256 of a token, as stored in the
// tokens file.
func Hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// AppendToken appends a "name scope hash" line to the tokens file at path,
// creating it with 0600 permissions if it does not exist. It fails if a token
// with the same name already exists (parseFile rejects duplicate names, which
// would otherwise lock everyone out on the next reload) or if the existing file
// cannot be read or written — callers that lack the privileges to write the
// file (i.e. not running under doas/root) should fall back to printing the line.
func AppendToken(path, name string, scope Scope, hash string) error {
	if path == "" {
		return errors.New("empty tokens file path")
	}
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("invalid token name %q (no whitespace allowed)", name)
	}
	if scope != ScopeRead && scope != ScopeWrite {
		return fmt.Errorf("invalid scope %q (use %q or %q)", scope, ScopeRead, ScopeWrite)
	}

	// Reject a duplicate name up front. parseFile returns (nil, nil) when the
	// file does not exist yet, so a fresh file is fine.
	existing, err := parseFile(path)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.name == name {
			return fmt.Errorf("a token named %q already exists in %s", name, path)
		}
	}

	// Detect a missing final newline so the appended line never glues onto a
	// previous one.
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	needLeadingNL := len(data) > 0 && data[len(data)-1] != '\n'

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	var b strings.Builder
	if needLeadingNL {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%s\t%s\t%s\n", name, scope, hash)
	if _, err := f.WriteString(b.String()); err != nil {
		return err
	}
	return nil
}

// GenerateToken returns a new random token of the form "pfw_<base64url>".
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}
