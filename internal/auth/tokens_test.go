package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTokens(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tokens file: %v", err)
	}
	return path
}

func TestEnvTokenIsWriteScoped(t *testing.T) {
	s, err := New("", "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	name, scope, ok := s.Lookup("secret")
	if !ok || name != "env" || scope != ScopeWrite {
		t.Fatalf("env token: got (%q, %q, %v), want (env, write, true)", name, scope, ok)
	}
	if _, _, ok := s.Lookup("wrong"); ok {
		t.Fatal("unexpected match for wrong token")
	}
	if s.Empty() {
		t.Fatal("store with env token should not be Empty")
	}
}

func TestEmptyStore(t *testing.T) {
	s, err := New("", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !s.Empty() {
		t.Fatal("store with no tokens should be Empty")
	}
	if _, _, ok := s.Lookup("anything"); ok {
		t.Fatal("empty store should not authenticate")
	}
}

func TestFileTokensAndScopes(t *testing.T) {
	tokWrite := "pfw_terraform"
	tokRead := "pfw_monitor"
	content := "# comment\n\n" +
		"terraform  write  " + Hash(tokWrite) + "\n" +
		"monitor    read   " + Hash(tokRead) + "\n"
	path := writeTokens(t, content)

	s, err := New(path, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Count() != 2 {
		t.Fatalf("Count = %d, want 2", s.Count())
	}

	if _, scope, ok := s.Lookup(tokWrite); !ok || scope != ScopeWrite {
		t.Fatalf("write token: got (%q, %v)", scope, ok)
	}
	if _, scope, ok := s.Lookup(tokRead); !ok || scope != ScopeRead {
		t.Fatalf("read token: got (%q, %v)", scope, ok)
	}
}

func TestReloadPicksUpChanges(t *testing.T) {
	tok1 := "pfw_one"
	tok2 := "pfw_two"
	path := writeTokens(t, "a write "+Hash(tok1)+"\n")

	s, err := New(path, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, ok := s.Lookup(tok2); ok {
		t.Fatal("tok2 should not match before reload")
	}

	if err := os.WriteFile(path, []byte("b write "+Hash(tok2)+"\n"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, _, ok := s.Lookup(tok2); !ok {
		t.Fatal("tok2 should match after reload")
	}
	if _, _, ok := s.Lookup(tok1); ok {
		t.Fatal("tok1 should be gone after reload")
	}
}

func TestReloadKeepsPreviousSetOnError(t *testing.T) {
	tok := "pfw_keep"
	path := writeTokens(t, "a write "+Hash(tok)+"\n")
	s, err := New(path, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Corrupt the file: invalid scope.
	if err := os.WriteFile(path, []byte("a bogus "+Hash(tok)+"\n"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if err := s.Reload(); err == nil {
		t.Fatal("expected reload error on invalid scope")
	}
	// Previous (valid) token must still authenticate.
	if _, _, ok := s.Lookup(tok); !ok {
		t.Fatal("previous token set should be kept after a failed reload")
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "does-not-exist"), "")
	if err != nil {
		t.Fatalf("New with missing file should not error: %v", err)
	}
	if !s.Empty() {
		t.Fatal("missing file yields no tokens")
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"too few fields": "name write\n",
		"bad scope":      "name admin " + Hash("x") + "\n",
		"bad hash":       "name write nothex\n",
		"duplicate name": "a write " + Hash("x") + "\na read " + Hash("y") + "\n",
	}
	for desc, content := range cases {
		path := writeTokens(t, content)
		if _, err := New(path, ""); err == nil {
			t.Errorf("%s: expected error, got nil", desc)
		}
	}
}

func TestAppendTokenCreatesAndLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens") // does not exist yet
	tok := "pfw_appended"

	if err := AppendToken(path, "client", ScopeWrite, Hash(tok)); err != nil {
		t.Fatalf("AppendToken: %v", err)
	}
	// The file the helper created must parse and authenticate.
	s, err := New(path, "")
	if err != nil {
		t.Fatalf("New after append: %v", err)
	}
	if _, scope, ok := s.Lookup(tok); !ok || scope != ScopeWrite {
		t.Fatalf("appended token: got (%q, %v), want (write, true)", scope, ok)
	}
}

func TestAppendTokenToExistingFileWithoutTrailingNewline(t *testing.T) {
	tok1 := "pfw_first"
	tok2 := "pfw_second"
	// No trailing newline on the existing line.
	path := writeTokens(t, "first write "+Hash(tok1))

	if err := AppendToken(path, "second", ScopeRead, Hash(tok2)); err != nil {
		t.Fatalf("AppendToken: %v", err)
	}
	s, err := New(path, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Count() != 2 {
		t.Fatalf("Count = %d, want 2 (lines must not be glued together)", s.Count())
	}
}

func TestAppendTokenRejectsDuplicateNameAndBadInput(t *testing.T) {
	path := writeTokens(t, "client write "+Hash("pfw_x")+"\n")

	if err := AppendToken(path, "client", ScopeWrite, Hash("pfw_y")); err == nil {
		t.Fatal("expected error appending a duplicate token name")
	}
	if err := AppendToken(path, "bad name", ScopeWrite, Hash("pfw_z")); err == nil {
		t.Fatal("expected error for a name containing whitespace")
	}
	if err := AppendToken(path, "ok", "admin", Hash("pfw_z")); err == nil {
		t.Fatal("expected error for an invalid scope")
	}
}

func TestGenerateTokenFormat(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(tok) < 10 || tok[:4] != "pfw_" {
		t.Fatalf("token %q has unexpected format", tok)
	}
	other, _ := GenerateToken()
	if tok == other {
		t.Fatal("two generated tokens should differ")
	}
}
