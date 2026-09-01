package collect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// codexWindow uses the cache, then a gpt-5/empty fallback, else zero.
// Steps:
// 1. Build a one-entry window map.
// 2. Call codexWindow for a hit, empty model, gpt-5 miss, and unknown slug.
// 3. Expect 258400, fallback 258400, fallback 258400, and 0.
func TestCodexWindow(t *testing.T) {
	win := map[string]int64{"gpt-5.6-terra": 258400}
	if g := codexWindow("gpt-5.6-terra", win); g != 258400 {
		t.Fatalf("hit %d", g)
	}
	if g := codexWindow("", win); g != 272000*95/100 {
		t.Fatalf("empty %d", g)
	}
	if g := codexWindow("gpt-5.4-missing", win); g != 272000*95/100 {
		t.Fatalf("gpt-5 miss %d", g)
	}
	if g := codexWindow("mystery-model", win); g != 0 {
		t.Fatalf("unknown %d", g)
	}
}

func TestCodexCtxOmitsCumulativeTokens(t *testing.T) {
	win := map[string]int64{"gpt-5.6-terra": 258400}
	if got := codexCtx(129200, "gpt-5.6-terra", win); got != "50%" {
		t.Fatalf("within window: %q", got)
	}
	if got := codexCtx(12_798_336, "gpt-5.6-terra", win); got != "" {
		t.Fatalf("cumulative tokens should not be ctx: %q", got)
	}
}

func TestLiveSessionAgents(t *testing.T) {
	got := liveSessionAgents([]Session{
		{Agent: "claude", Live: true},
		{Agent: "codex", Live: false},
		{Agent: "grok", Live: true},
	})
	if !got["claude"] || !got["grok"] || got["codex"] {
		t.Fatalf("got %#v", got)
	}
}

// loadCodexWindows maps only valid slugs; percent applies for 1–99.
// Steps:
// 1. Write each models_cache.json fixture (or omit the file).
// 2. Call loadCodexWindows.
// 3. Expect max-window fallback, percent only for 1–99, and omitted invalid entries.
func TestLoadCodexWindows(t *testing.T) {
	writeCache := func(t *testing.T, home, body string) {
		t.Helper()
		dir := filepath.Join(home, ".codex")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wantOnly := func(slug string, window int64) map[string]int64 {
		return map[string]int64{slug: window}
	}

	t.Run("nominal95", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":272000,"max_context_window":272000,"effective_context_window_percent":95}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 272000*95/100)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("zeroWindowFallsBackToMax", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":0,"max_context_window":272000,"effective_context_window_percent":95}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 272000*95/100)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("negativeWindowFallsBackToMax", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":-1,"max_context_window":200000,"effective_context_window_percent":50}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 200000*50/100)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("effective0KeepsWindow", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":100000,"max_context_window":272000,"effective_context_window_percent":0}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 100000)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("effective100KeepsWindow", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":100000,"max_context_window":272000,"effective_context_window_percent":100}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 100000)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("effective1Applies", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":100000,"effective_context_window_percent":1}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 1000)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("effective99Applies", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"gpt-5.6-terra","context_window":100000,"effective_context_window_percent":99}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("gpt-5.6-terra", 99000)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("zeroWindowAndMaxOmitted", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"bad-win","context_window":0,"max_context_window":0,"effective_context_window_percent":95}]}`)
		got := loadCodexWindows(home)
		if len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("emptySlugOmitted", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[{"slug":"","context_window":272000,"effective_context_window_percent":95},{"slug":"ok","context_window":200000}]}`)
		got := loadCodexWindows(home)
		want := wantOnly("ok", 200000)
		if !mapsEqual(got, want) {
			t.Fatalf("got %v want %v", got, want)
		}
	})
	t.Run("malformedJSON", func(t *testing.T) {
		home := t.TempDir()
		writeCache(t, home, `{"models":[`)
		if got := loadCodexWindows(home); got != nil {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("missingFile", func(t *testing.T) {
		if got := loadCodexWindows(t.TempDir()); got != nil {
			t.Fatalf("got %v", got)
		}
	})
}

func mapsEqual(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
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
	if !strings.Contains(got, "char(31)") {
		t.Fatalf("USV strip missing: %q", got)
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
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub")
	}
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

// querySQLiteMaps USV fallback keeps model when title contains the delimiter.
// Steps:
// 1. Write a thread whose title includes U+001F and force sqlite3 to reject -json.
// 2. Call enrichCodex with a known model window.
// 3. Expect tokens_used, model, and a 50% CTX label.
func TestQuerySQLiteMapsFallsBackWhenTitleContainsUSV(t *testing.T) {
	home := t.TempDir()
	writeCodexFixture(t, home, threadsSchema+`
INSERT INTO threads VALUES ('t1','/tmp/usv','hello' || char(31) || 'world',129200,0,2000000000,'gpt-5.6-terra');
`)
	forceSQLiteUSVFallback(t)
	s := Session{}
	enrichCodex(&s, home, "/tmp/usv", "", map[string]int64{"gpt-5.6-terra": 258400})
	if s.Tokens == nil || *s.Tokens != 129200 || s.Ctx != "50%" {
		t.Fatalf("got %#v", s)
	}
}

func forceSQLiteUSVFallback(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not on PATH")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = \"-json\" ]; then echo 'unknown option: -json' >&2; exit 1; fi\n" +
		"done\n" +
		"exec '" + strings.ReplaceAll(real, "'", "'\\''") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlite3"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
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

// isCodexExec treats exec as an argv token and skips known value-taking globals.
// Steps:
// 1. Feed genuine exec argv, value-taking globals in separated and equals form, and lookalikes.
// 2. Call isCodexExec.
// 3. Expect true only when the first positional token is exec (or alias e).
func TestIsCodexExec(t *testing.T) {
	yes := []string{
		"codex exec --json",
		"codex\x00exec\x00--cd\x00/tmp",
		"codex --profile work exec",
		"codex\x00--profile\x00work\x00exec",
		"codex\x00--profile=work\x00exec",
		"codex --config model=o3 exec",
		"codex\x00--config=model=o3\x00exec",
		"codex -C /tmp exec",
		"codex --search exec",
		"codex -- e",
		"codex e --json",
	}
	for _, cmd := range yes {
		if !isCodexExec(cmd) {
			t.Fatalf("want exec: %q", cmd)
		}
	}
	no := []string{
		"codex",
		"codex resume",
		"codex please exec something",
		"codex\x00--cd\x00/work/My exec dir\x00resume",
		"codex --profile work resume",
		"codex\x00--profile=work\x00resume",
	}
	for _, cmd := range no {
		if isCodexExec(cmd) {
			t.Fatalf("want non-exec: %q", cmd)
		}
	}
}

// leftoverSession marks a Codex exec process busy and titles it exec when SQLite has no title.
// Steps:
// 1. Build Codex exec Procs, including value-taking globals before exec in both argv forms.
// 2. Call leftoverSession.
// 3. Expect status busy and title exec.
func TestLeftoverSessionCodexExecFallback(t *testing.T) {
	home := t.TempDir()
	cmds := []string{
		"codex exec --json",
		"codex\x00--profile\x00work\x00exec",
		"codex\x00--profile=work\x00exec",
		"codex\x00--config\x00model=o3\x00exec",
	}
	for _, cmd := range cmds {
		p := Proc{PID: 42, Agent: "codex", CWD: "/tmp/no-thread", Cmd: cmd, Raw: cmd}
		s := leftoverSession(p, home, nil)
		if s.Status != "busy" || s.Title != "exec" {
			t.Fatalf("cmd %q status=%q title=%q", cmd, s.Status, s.Title)
		}
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

// normalizeStatus maps vendor status strings onto busy/run/wait/idle.
func TestNormalizeStatus(t *testing.T) {
	cases := map[string]string{
		"": "idle", "idle": "idle", "busy": "busy", "shell": "busy",
		"working": "busy", "wait": "wait", "waiting": "wait",
		"run": "run", "other": "run",
	}
	for in, want := range cases {
		if g := normalizeStatus(in); g != want {
			t.Fatalf("normalizeStatus(%q)=%q want %q", in, g, want)
		}
	}
}

// sessionFresh treats millisecond timestamps as recent within the window.
func TestSessionFresh(t *testing.T) {
	now := time.Now().UnixMilli()
	if !sessionFresh(now, 2*time.Hour) {
		t.Fatal("fresh ms")
	}
	if sessionFresh(now-int64(3*time.Hour/time.Millisecond), 2*time.Hour) {
		t.Fatal("stale ms")
	}
	if sessionFresh(0, 2*time.Hour) {
		t.Fatal("zero")
	}
	if !sessionFresh(time.Now().Unix(), 2*time.Hour) {
		t.Fatal("fresh seconds")
	}
}

// lastUsage looks past an 8KiB tail of non-usage events.
func TestLastUsageRetriesLargerTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	var b strings.Builder
	b.WriteString(`{"message":{"model":"claude-sonnet-5","usage":{"input_tokens":10,"cache_read_input_tokens":90,"cache_creation_input_tokens":0}}}` + "\n")
	pad := `{"type":"system","content":"x"}` + "\n"
	for b.Len() < 9000 {
		b.WriteString(pad)
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, model, ok := lastUsage(p)
	if !ok || tok != 100 || model != "claude-sonnet-5" {
		t.Fatalf("got %v %q %v", tok, model, ok)
	}
	if _, _, ok := lastUsageTail(p, 8192); ok {
		t.Fatal("8KiB tail should miss usage")
	}
}

// parseOpenCodeModel reads a slug or the id field of a JSON model object.
func TestParseOpenCodeModel(t *testing.T) {
	if g := parseOpenCodeModel(`{"id":"nemotron-3-ultra-free","providerID":"opencode"}`); g != "nemotron-3-ultra-free" {
		t.Fatalf("json: %q", g)
	}
	if g := parseOpenCodeModel("gpt-4"); g != "gpt-4" {
		t.Fatalf("slug: %q", g)
	}
	if g := parseOpenCodeModel(""); g != "" {
		t.Fatalf("empty: %q", g)
	}
}

const openCodeSchema = `CREATE TABLE session (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL DEFAULT '',
  slug TEXT NOT NULL DEFAULT '',
  directory TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  version TEXT NOT NULL DEFAULT '',
  time_created INTEGER NOT NULL DEFAULT 0,
  time_updated INTEGER NOT NULL DEFAULT 0,
  time_archived INTEGER,
  model TEXT,
  tokens_input INTEGER NOT NULL DEFAULT 0,
  tokens_cache_read INTEGER NOT NULL DEFAULT 0
);
`

func writeOpenCodeFixture(t *testing.T, home, sql string) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not on PATH")
	}
	data := filepath.Join(home, "xdg")
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "opencode.db")
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v %s", err, out)
	}
}

// enrichOpenCode fills title, model, and context-side tokens from one session row.
func TestEnrichOpenCode(t *testing.T) {
	home := t.TempDir()
	writeOpenCodeFixture(t, home, openCodeSchema+`
INSERT INTO session (id, directory, title, model, tokens_input, tokens_cache_read, time_updated)
VALUES ('s1','/tmp/ocproj','hello','{"id":"nemotron-3-ultra-free"}',100,50,2000000000000);
`)
	s := Session{}
	enrichOpenCode(&s, home, "/tmp/ocproj")
	if s.Title != "hello" || s.Model != "nemotron-3-ultra-free" || s.Tokens == nil || *s.Tokens != 150 {
		t.Fatalf("got %#v", s)
	}
}

// recentOpenCodeIdle lists a recently updated unused session.
func TestRecentOpenCodeIdle(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UnixMilli()
	writeOpenCodeFixture(t, home, openCodeSchema+
		"INSERT INTO session (id, directory, title, model, tokens_input, tokens_cache_read, time_updated) VALUES ('s1','/tmp/ocidle','ok','m',20,5,"+strconv.FormatInt(now, 10)+");\n")
	got := recentOpenCodeIdle(home, nil, 2*time.Hour)
	if len(got) != 1 || got[0].Dir != "ocidle" || got[0].Model != "m" || got[0].Tokens == nil || *got[0].Tokens != 25 {
		t.Fatalf("got %#v", got)
	}
}

// claudeSessions includes a recent idle session whose pid is not a live claude.
func TestClaudeRecentIdle(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	dir := filepath.Join(cfg, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"pid":2147483646,"sessionId":"sid-idle","cwd":"/tmp/clidle","status":"shell","name":"parked","updatedAt":%d}`, time.Now().UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, "2147483646.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := claudeSessions(home, nil, map[int]bool{}, true, 2*time.Hour)
	if len(got) != 1 || got[0].Live || got[0].Status != "idle" || got[0].Dir != "clidle" || got[0].Title != "parked" {
		t.Fatalf("got %#v", got)
	}
	if n := claudeSessions(home, nil, map[int]bool{}, false, 2*time.Hour); len(n) != 0 {
		t.Fatalf("live-only %#v", n)
	}
}

func TestWantAgent(t *testing.T) {
	all := Options{}
	if !all.want("claude") {
		t.Fatal("empty agents means all")
	}
	one := Options{Agents: []string{"codex"}}
	if one.want("claude") || !one.want("codex") {
		t.Fatal("filter")
	}
}
