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

func TestClaudeTailTokens(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	sid := "sess-config"
	// decoy in $HOME/.claude — must not be used
	decoy := filepath.Join(home, ".claude", "projects", "decoy")
	if err := os.MkdirAll(decoy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, sid+".jsonl"), []byte(
		`{"message":{"usage":{"input_tokens":9,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	realDir := filepath.Join(cfg, "projects", "real")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, sid+".jsonl"), []byte(
		`{"message":{"model":"claude-opus-5","usage":{"input_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":8}}}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	got := claudeTailTokens(home, sid)
	if got == nil || *got != 110 {
		t.Fatalf("configured tree tokens %#v", got)
	}
	tok, model, ok := claudeTail(home, sid)
	if !ok || tok != 110 || model != "claude-opus-5" {
		t.Fatalf("claudeTail=%v %q %v", tok, model, ok)
	}
	if claudeTailTokens(home, "missing-id") != nil {
		t.Fatal("missing sid should be nil")
	}
}

func TestClaudeWindow(t *testing.T) {
	if claudeWindow("claude-opus-5") != 200_000 {
		t.Fatal("default window")
	}
	if claudeWindow("claude-sonnet-4-5-1m") != 1_000_000 {
		t.Fatal("1m window")
	}
	if ctxPct(100_000, 200_000) != "50%" {
		t.Fatalf("ctxPct=%s", ctxPct(100_000, 200_000))
	}
}

func TestParseSQLiteJSONKeepsModelAcrossTitleNewlines(t *testing.T) {
	raw := []byte(`[
	  {"tokens_used":56000,"title":"schema_version: 1\nworking_dir: /tmp","cwd":"/home/u/github/agent-usage","model":"gpt-5.6-terra"}
	]`)
	rows := parseSQLiteJSON(raw)
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0]["model"] != "gpt-5.6-terra" || rows[0]["tokens_used"] != "56000" {
		t.Fatalf("got %#v", rows[0])
	}
	if tidyTitle(rows[0]["title"]) != "" {
		t.Fatalf("title %q", tidyTitle(rows[0]["title"]))
	}
	if w := codexWindow(rows[0]["model"], map[string]int64{"gpt-5.6-terra": 258400}); w != 258400 {
		t.Fatalf("window %d", w)
	}
	if ctxPct(56000, 258400) != "21%" {
		t.Fatalf("ctx %s", ctxPct(56000, 258400))
	}
}

func TestLoadCodexWindows(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"models":[{"slug":"gpt-5.6-terra","context_window":272000,"max_context_window":272000,"effective_context_window_percent":95}]}`
	if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	w := loadCodexWindows(home)
	if w["gpt-5.6-terra"] != 272000*95/100 {
		t.Fatalf("got %v", w)
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
