package collect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShortPathBasename(t *testing.T) {
	if g := shortPath("/tmp/foo/bar"); g != "bar" {
		t.Fatalf("got %s", g)
	}
}

func TestLastUsageTokens(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	body := "{\"type\":\"user\"}\n" +
		`{"type":"assistant","message":{"usage":{"input_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":8}}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := lastUsageTokens(p)
	if got == nil || *got != 110 {
		t.Fatalf("got %#v", got)
	}
}

func TestGrokSignalsParse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "signals.json"),
		[]byte(`{"contextWindowUsage":35,"contextTokensUsed":176721}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := readGrokSignals(dir)
	if s == nil || s.ContextWindowUsage != 35 || s.ContextTokensUsed != 176721 {
		t.Fatalf("got %#v", s)
	}
}

func TestEscapeSQL(t *testing.T) {
	if g := escapeSQL("a'b"); g != "a''b" {
		t.Fatalf("got %s", g)
	}
}
