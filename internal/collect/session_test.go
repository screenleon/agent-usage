package collect

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// shortPath returns the last path element for display.
// Steps:
// 1. Pass an absolute directory.
// 2. Call shortPath.
// 3. Expect only the final element.
func TestShortPathBasename(t *testing.T) {
	if g := shortPath("/tmp/foo/bar"); g != "bar" {
		t.Fatalf("got %s", g)
	}
}

// claudeTailTokens reads usage from CLAUDE_CONFIG_DIR, not $HOME/.claude.
// Steps:
// 1. Plant different jsonl files under HOME and CLAUDE_CONFIG_DIR.
// 2. Call claudeTailTokens with that HOME.
// 3. Expect tokens and model from the configured tree only.
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

// claudeWindow selects 200k unless the model name contains 1m.
// Steps:
// 1. Choose default and 1m model ids.
// 2. Call claudeWindow and ctxPct.
// 3. Expect 200000, 1000000, and a 50% label.
func TestClaudeWindow(t *testing.T) {
	if claudeWindow("claude-opus-5") != 200_000 {
		t.Fatal("default window")
	}
	if claudeWindow("claude-sonnet-4-5-1m") != 1_000_000 {
		t.Fatal("1m window")
	}
}

// ctxPct rejects invalid inputs and caps percentages at 999.
// Steps:
// 1. Call ctxPct with negative usage, a zero window, an exact full window, and a huge overage.
// 2. Compare each result to the documented label.
// 3. Expect empty strings for invalid input, 100% at the boundary, and 999% when capped.
func TestCtxPct(t *testing.T) {
	if ctxPct(-1, 200_000) != "" {
		t.Fatal("negative usage")
	}
	if ctxPct(100, 0) != "" {
		t.Fatal("zero window")
	}
	if ctxPct(200_000, 200_000) != "100%" {
		t.Fatal("exact boundary")
	}
	if ctxPct(1e12, 1) != "999%" {
		t.Fatal("over cap")
	}
	if ctxPct(100_000, 200_000) != "50%" {
		t.Fatal("happy")
	}
}

// parseSQLiteJSON keeps the model column when title contains newlines.
// Steps:
// 1. Decode a JSON row whose title includes embedded newlines.
// 2. Read model, tokens, and tidyTitle.
// 3. Expect the model slug and a 21% ctx for a 258400 window.
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

// loadCodexWindows applies the effective percent to context_window.
// Steps:
// 1. Write a models_cache.json with a 95% effective window.
// 2. Call loadCodexWindows.
// 3. Expect 272000*95/100 for gpt-5.6-terra.
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

// lastUsageTokens sums the last assistant usage buckets from a jsonl tail.
// Steps:
// 1. Write a two-line jsonl with a trailing usage object.
// 2. Call lastUsageTokens.
// 3. Expect 110 tokens.
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

// readGrokSignals loads context usage from signals.json.
// Steps:
// 1. Write a signals.json fixture.
// 2. Call readGrokSignals.
// 3. Expect the stored usage and token counts.
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

// escapeSQL doubles single quotes for sqlite3 string literals.
// Steps:
// 1. Pass a value containing a quote.
// 2. Call escapeSQL.
// 3. Expect the quote to be doubled.
// parseSQLiteUSV maps header and unit-separated rows after a -json fallback.
// Steps:
// 1. Feed a two-line USV table with tokens, title, and model.
// 2. Call parseSQLiteUSV.
// 3. Expect one map with those column names.
func TestParseSQLiteUSV(t *testing.T) {
	raw := []byte("tokens_used\x1ftitle\x1fmodel\n56000\x1fok\x1fgpt-5.6-terra\n")
	rows := parseSQLiteUSV(raw)
	if len(rows) != 1 || rows[0]["model"] != "gpt-5.6-terra" || rows[0]["tokens_used"] != "56000" {
		t.Fatalf("got %#v", rows)
	}
}

func TestEscapeSQL(t *testing.T) {
	if g := escapeSQL("a'b"); g != "a''b" {
		t.Fatalf("got %s", g)
	}
}

// flattenSQLNewlines aliases the rewritten title expression back to title.
// Steps:
// 1. Take a SELECT that lists title among other columns.
// 2. Call flattenSQLNewlines.
// 3. Expect the title expression to end with AS title.
func TestFlattenSQLNewlinesKeepsTitleAlias(t *testing.T) {
	q := `SELECT tokens_used, title, cwd, IFNULL(model,'') AS model FROM threads`
	got := flattenSQLNewlines(q)
	if !strings.Contains(got, "AS title") || !strings.Contains(got, "tokens_used") {
		t.Fatalf("got %q", got)
	}
	if strings.Count(got, ", title,") != 0 {
		t.Fatalf("bare title column remains: %q", got)
	}
}

// querySQLiteMaps uses the USV fallback when sqlite3 rejects -json.
// Steps:
// 1. Put a stub sqlite3 on PATH that fails -json and prints a USV table.
// 2. Call querySQLiteMaps against a dummy db path.
// 3. Expect tokens_used, title, and model from the fallback row.
func TestQuerySQLiteMapsFallsBackWhenJSONUnsupported(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "state_5.sqlite")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "sqlite3")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = \"-json\" ]; then echo 'unknown option: -json' >&2; exit 1; fi\n" +
		"done\n" +
		"printf 'tokens_used\\037title\\037model\\n56000\\037hello world\\037gpt-5.6-terra\\n'\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	rows := querySQLiteMaps(db, `SELECT tokens_used, title, IFNULL(model,'') AS model FROM threads`, "/tmp/x")
	if len(rows) != 1 || rows[0]["title"] != "hello world" || rows[0]["model"] != "gpt-5.6-terra" || rows[0]["tokens_used"] != "56000" {
		t.Fatalf("got %#v", rows)
	}
}

func writeCodexFixture(t *testing.T, home, sql string) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not on PATH")
	}
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "state_5.sqlite")
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v %s", err, out)
	}
}

const threadsSchema = `CREATE TABLE threads (
  id TEXT PRIMARY KEY,
  cwd TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  tokens_used INTEGER NOT NULL DEFAULT 0,
  archived INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0,
  model TEXT
);
`

// enrichCodex sets Ctx from the matched model window, leaves it empty for
// unknown models, and uses the gpt-5 fallback when model is missing.
// Steps:
// 1. Create a state_5.sqlite with matched, unknown, and empty-model rows.
// 2. Call enrichCodex for each cwd.
// 3. Expect 50%, empty Ctx, and the fallback percent respectively.
func TestEnrichCodex(t *testing.T) {
	home := t.TempDir()
	writeCodexFixture(t, home, threadsSchema+`
INSERT INTO threads VALUES ('t1','/tmp/matched','ok',129200,0,2000000000,'gpt-5.6-terra');
INSERT INTO threads VALUES ('t2','/tmp/unknown','ok',100000,0,2000000000,'mystery-model');
INSERT INTO threads VALUES ('t3','/tmp/fallback','ok',129200,0,2000000000,'');
`)
	win := map[string]int64{"gpt-5.6-terra": 258400}

	matched := Session{}
	enrichCodex(&matched, home, "/tmp/matched", "", win)
	if matched.Tokens == nil || *matched.Tokens != 129200 || matched.Ctx != "50%" {
		t.Fatalf("matched %#v", matched)
	}

	unknown := Session{}
	enrichCodex(&unknown, home, "/tmp/unknown", "", win)
	if unknown.Tokens == nil || *unknown.Tokens != 100000 || unknown.Ctx != "" {
		t.Fatalf("unknown %#v", unknown)
	}

	fallback := Session{}
	enrichCodex(&fallback, home, "/tmp/fallback", "", win)
	if fallback.Tokens == nil || fallback.Ctx != "50%" {
		t.Fatalf("fallback %#v", fallback)
	}

	fromFlag := Session{}
	enrichCodex(&fromFlag, home, "/tmp/no-row", "codex\x00exec\x00-m\x00gpt-5.6-terra", win)
	// no tokens from sqlite; Ctx stays empty
	if fromFlag.Tokens != nil {
		t.Fatalf("no-row should not invent tokens: %#v", fromFlag)
	}
}

// leftoverSession marks a Codex exec process busy and titles it exec when SQLite has no title.
// Steps:
// 1. Build a Codex exec Proc whose cwd has no matching sqlite row.
// 2. Call leftoverSession.
// 3. Expect status busy and title exec.
func TestLeftoverSessionCodexExecFallback(t *testing.T) {
	home := t.TempDir()
	p := Proc{PID: 42, Agent: "codex", CWD: "/tmp/no-thread", Cmd: "codex exec --json"}
	s := leftoverSession(p, home, nil)
	if s.Status != "busy" || s.Title != "exec" {
		t.Fatalf("got status=%q title=%q", s.Status, s.Title)
	}
}

// recentCodexIdle reports Ctx for recent unused Codex threads.
// Steps:
// 1. Insert a recent thread under /tmp/idleproj with a known model.
// 2. Call recentCodexIdle with an empty live set.
// 3. Expect one idle session whose Ctx is 50%.
func TestRecentCodexIdle(t *testing.T) {
	home := t.TempDir()
	now := time.Now().Unix()
	writeCodexFixture(t, home, threadsSchema+
		"INSERT INTO threads VALUES ('t1','/tmp/idleproj','ok',129200,0,"+strconv.FormatInt(now, 10)+",'gpt-5.6-terra');\n")
	win := map[string]int64{"gpt-5.6-terra": 258400}
	got := recentCodexIdle(home, nil, 2*time.Hour, win)
	if len(got) != 1 || got[0].Ctx != "50%" || got[0].Dir != "idleproj" {
		t.Fatalf("got %#v", got)
	}
}
