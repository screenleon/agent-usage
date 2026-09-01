package collect

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/screenleon/agent-usage/internal/filter"
)

type Session struct {
	Live    bool     `json:"live"`
	Status  string   `json:"status"`
	Agent   string   `json:"agent"`
	PID     int      `json:"pid,omitempty"`
	CPU     float64  `json:"cpu,omitempty"`
	RSSKB   uint64   `json:"rss_kb,omitempty"`
	Elapsed string   `json:"elapsed,omitempty"`
	Ctx     string   `json:"ctx,omitempty"`
	Tokens  *float64 `json:"tokens,omitempty"`
	Model   string   `json:"model,omitempty"`
	Kids    int      `json:"kids,omitempty"`
	Dir     string   `json:"dir"`
	Title   string   `json:"title,omitempty"`
}

type Snapshot struct {
	Taken    time.Time `json:"taken"`
	Sessions []Session `json:"sessions"`
}

type Options struct {
	Home      string
	Recent    bool
	RecentFor time.Duration
	Agents    []string
}

func (o Options) want(agent string) bool {
	return filter.Wants(o.Agents, agent)
}

func Collect(opt Options) Snapshot {
	if opt.Home == "" {
		opt.Home = os.Getenv("HOME")
	}
	if opt.RecentFor == 0 {
		opt.RecentFor = 2 * time.Hour
	}
	procs := liveAgentProcs()
	codexWin := loadCodexWindows(opt.Home)
	var rows []Session

	used := map[int]bool{}
	if opt.want("claude") {
		rows = append(rows, claudeSessions(opt.Home, procs, used, opt.Recent, opt.RecentFor)...)
	}
	if opt.want("grok") {
		rows = append(rows, grokSessions(opt.Home, procs, used, opt.Recent)...)
	}
	for pid, p := range procs {
		if used[pid] || !opt.want(p.Agent) {
			continue
		}
		s := leftoverSession(p, opt.Home, codexWin)
		rows = append(rows, s)
		used[pid] = true
	}
	if opt.Recent {
		if opt.want("codex") {
			rows = append(rows, recentCodexIdle(opt.Home, rows, opt.RecentFor, codexWin)...)
		}
		if opt.want("opencode") {
			rows = append(rows, recentOpenCodeIdle(opt.Home, rows, opt.RecentFor)...)
		}
	}
	return Snapshot{Taken: time.Now(), Sessions: rows}
}

func leftoverSession(p Proc, home string, windows map[string]int64) Session {
	st := "run"
	src := p.Raw
	if src == "" {
		src = p.Cmd
	}
	exec := p.Agent == "codex" && isCodexExec(src)
	if exec {
		st = "busy"
	}
	s := sessionFromProc(p, st)
	switch p.Agent {
	case "codex":
		enrichCodex(&s, home, p.CWD, src, windows)
		if s.Title == "" && exec {
			s.Title = "exec"
		}
	case "opencode":
		enrichOpenCode(&s, home, p.CWD)
	}
	return s
}

// Codex global flags that consume the next argv token (clap value options).
// Boolean flags are omitted so their following token can still be the subcommand.
var codexValueFlags = map[string]bool{
	"-c": true, "--config": true,
	"--enable": true, "--disable": true,
	"--remote": true, "--remote-auth-token-env": true,
	"-i": true, "--image": true,
	"-m": true, "--model": true,
	"--local-provider": true,
	"-p":               true, "--profile": true,
	"-s": true, "--sandbox": true,
	"-C": true, "--cd": true,
	"--add-dir": true,
	"-a":        true, "--ask-for-approval": true,
}

func isCodexExec(cmd string) bool {
	args := argv(cmd)
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return isExecToken(args[i+1])
			}
			return false
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && codexValueFlags[a] {
				i++
			}
			continue
		}
		return isExecToken(a)
	}
	return false
}

func isExecToken(a string) bool {
	return a == "exec" || a == "e"
}

func sessionFromProc(p Proc, st string) Session {
	return Session{
		Live:    true,
		Status:  normalizeStatus(st),
		Agent:   p.Agent,
		PID:     p.PID,
		CPU:     p.CPU,
		RSSKB:   p.RSSKB,
		Elapsed: p.Elapsed,
		Kids:    p.Kids,
		Dir:     shortPath(p.CWD),
	}
}

func normalizeStatus(st string) string {
	switch strings.ToLower(strings.TrimSpace(st)) {
	case "busy", "shell", "working":
		return "busy"
	case "wait", "waiting":
		return "wait"
	case "idle", "":
		return "idle"
	case "run":
		return "run"
	default:
		return "run"
	}
}

func claudeSessions(home string, procs map[int]Proc, used map[int]bool, recent bool, recentFor time.Duration) []Session {
	dir := filepath.Join(claudeHome(home), "sessions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Session
	idleN := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var meta claudeSessionFile
		if json.Unmarshal(raw, &meta) != nil {
			continue
		}
		if meta.PID != 0 {
			pid = meta.PID
		}
		live := pidAliveAgent(pid, "claude")
		if !live {
			if !recent || idleN >= 8 || !sessionFresh(meta.UpdatedAt, recentFor) {
				continue
			}
		}
		st := meta.Status
		if st == "" {
			st = "idle"
		}
		var s Session
		if live {
			s = sessionFromProc(procs[pid], st)
			if s.PID == 0 {
				s.PID = pid
				s.Agent = "claude"
				s.Live = true
			}
			used[pid] = true
		} else {
			s = Session{Live: false, Status: "idle", Agent: "claude"}
			idleN++
		}
		if meta.CWD != "" {
			s.Dir = shortPath(meta.CWD)
		}
		s.Title = SanitizeDisplay(meta.Name)
		if tok, model, ok := claudeTail(home, meta.SessionID); ok {
			s.Tokens = &tok
			s.Ctx = ctxPct(tok, claudeWindow(model))
			s.Model = model
		}
		out = append(out, s)
	}
	return out
}

type claudeSessionFile struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Status    string `json:"status"`
	Name      string `json:"name"`
	UpdatedAt int64  `json:"updatedAt"`
}

func sessionFresh(ts int64, window time.Duration) bool {
	if ts <= 0 || window <= 0 {
		return false
	}
	return time.Since(time.Unix(unixSec(ts), 0)) <= window
}

func unixSec(ts int64) int64 {
	if ts > 1e12 {
		return ts / 1000
	}
	return ts
}

func claudeHome(home string) string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(home, ".claude")
}

func claudeTailTokens(home, sid string) *float64 {
	tok, _, ok := claudeTail(home, sid)
	if !ok {
		return nil
	}
	return &tok
}

func claudeTail(home, sid string) (tokens float64, model string, ok bool) {
	if sid == "" {
		return 0, "", false
	}
	root := filepath.Join(claudeHome(home), "projects")
	matches, _ := filepath.Glob(filepath.Join(root, "*", sid+".jsonl"))
	if len(matches) == 0 {
		return 0, "", false
	}
	return lastUsage(matches[0])
}

type tokenUsage struct {
	Input         float64 `json:"input_tokens"`
	CacheRead     float64 `json:"cache_read_input_tokens"`
	CacheCreation float64 `json:"cache_creation_input_tokens"`
}

func lastUsageTokens(path string) *float64 {
	tok, _, ok := lastUsage(path)
	if !ok {
		return nil
	}
	return &tok
}

func lastUsage(path string) (tokens float64, model string, ok bool) {
	// Context-side tokens only (input + cache). Do not scan the whole jsonl:
	// try 8KiB, then 32KiB, then 64KiB if the tail is trailing events without usage.
	for _, tail := range []int64{8192, 32768, 65536} {
		if tok, model, ok := lastUsageTail(path, tail); ok {
			return tok, model, true
		}
	}
	return 0, "", false
}

func lastUsageTail(path string, tail int64) (tokens float64, model string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, "", false
	}
	start := st.Size() - tail
	if start < 0 {
		start = 0
	}
	buf := make([]byte, st.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && len(buf) == 0 {
		return 0, "", false
	}
	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		var o struct {
			Model   string `json:"model"`
			Message *struct {
				Model string      `json:"model"`
				Usage *tokenUsage `json:"usage"`
			} `json:"message"`
			Usage *tokenUsage `json:"usage"`
		}
		if json.Unmarshal([]byte(ln), &o) != nil {
			continue
		}
		u := o.Usage
		model = o.Model
		if o.Message != nil {
			if o.Message.Usage != nil {
				u = o.Message.Usage
			}
			if o.Message.Model != "" {
				model = o.Message.Model
			}
		}
		if u == nil {
			continue
		}
		return u.Input + u.CacheRead + u.CacheCreation, model, true
	}
	return 0, "", false
}

func claudeWindow(model string) int64 {
	m := strings.ToLower(model)
	if strings.Contains(m, "1m") {
		return 1_000_000
	}
	return 200_000
}

func ctxPct(used float64, window int64) string {
	if window <= 0 || used < 0 {
		return ""
	}
	pct := int(used * 100 / float64(window))
	if pct > 999 {
		pct = 999
	}
	return strconv.Itoa(pct) + "%"
}

func loadCodexWindows(home string) map[string]int64 {
	b, err := os.ReadFile(filepath.Join(home, ".codex", "models_cache.json"))
	if err != nil {
		return nil
	}
	var raw struct {
		Models []struct {
			Slug   string `json:"slug"`
			Window int64  `json:"context_window"`
			MaxWin int64  `json:"max_context_window"`
			EffPct int64  `json:"effective_context_window_percent"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	out := make(map[string]int64, len(raw.Models))
	for _, m := range raw.Models {
		w := m.Window
		if w <= 0 {
			w = m.MaxWin
		}
		if m.EffPct > 0 && m.EffPct < 100 && w > 0 {
			w = w * m.EffPct / 100
		}
		if m.Slug != "" && w > 0 {
			out[m.Slug] = w
		}
	}
	return out
}

func grokSessions(home string, procs map[int]Proc, used map[int]bool, recent bool) []Session {
	raw, err := os.ReadFile(filepath.Join(home, ".grok", "active_sessions.json"))
	if err != nil {
		return nil
	}
	var items []struct {
		SessionID string `json:"session_id"`
		PID       int    `json:"pid"`
		CWD       string `json:"cwd"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []Session
	for _, it := range items {
		live := pidAliveAgent(it.PID, "grok")
		if !live && !recent {
			continue
		}
		st := "idle"
		p := procs[it.PID]
		if live {
			st = "run"
			for _, c := range childCmdlines(it.PID) {
				if strings.Contains(c, "turn in progress") {
					st = "busy"
					break
				}
			}
		}
		s := sessionFromProc(p, st)
		s.Live = live
		if s.PID == 0 {
			s.PID = it.PID
			s.Agent = "grok"
		}
		if !live {
			s.PID = 0
			s.CPU = 0
			s.RSSKB = 0
			s.Elapsed = ""
			s.Kids = 0
		}
		if it.CWD != "" {
			s.Dir = shortPath(it.CWD)
		}
		if dir := grokSessionDir(home, it.CWD, it.SessionID); dir != "" {
			applyGrokSignals(&s, readGrokSignals(dir))
			if sm := readGrokSummary(dir); sm != nil {
				s.Title = firstNonEmpty(sm.GeneratedTitle, sm.SessionSummary)
				s.Model = firstNonEmpty(s.Model, sm.CurrentModelID)
			}
		}
		out = append(out, s)
		if live {
			used[it.PID] = true
		}
	}
	return out
}

func grokSessionDir(home, cwd, sid string) string {
	if sid == "" {
		return ""
	}
	if cwd != "" {
		p := filepath.Join(home, ".grok", "sessions", url.QueryEscape(cwd), sid)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".grok", "sessions", "*", sid))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

type grokSignals struct {
	ContextWindowUsage  int    `json:"contextWindowUsage"`
	ContextTokensUsed   int64  `json:"contextTokensUsed"`
	ContextWindowTokens int64  `json:"contextWindowTokens"`
	PrimaryModelID      string `json:"primaryModelId"`
}

func applyGrokSignals(s *Session, sig *grokSignals) {
	if sig == nil {
		return
	}
	if sig.ContextTokensUsed > 0 {
		t := float64(sig.ContextTokensUsed)
		s.Tokens = &t
		if sig.ContextWindowTokens > 0 {
			s.Ctx = ctxPct(t, sig.ContextWindowTokens)
		}
	}
	if s.Ctx == "" && sig.ContextWindowUsage != 0 {
		s.Ctx = strconv.Itoa(sig.ContextWindowUsage) + "%"
	}
	s.Model = sig.PrimaryModelID
}

func readGrokSignals(dir string) *grokSignals {
	var s grokSignals
	if !readJSON(filepath.Join(dir, "signals.json"), &s) {
		return nil
	}
	return &s
}

type grokSummary struct {
	GeneratedTitle string `json:"generated_title"`
	SessionSummary string `json:"session_summary"`
	CurrentModelID string `json:"current_model_id"`
}

func readGrokSummary(dir string) *grokSummary {
	var s grokSummary
	if !readJSON(filepath.Join(dir, "summary.json"), &s) {
		return nil
	}
	return &s
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func enrichCodex(s *Session, home, cwd, cmd string, windows map[string]int64) {
	db := filepath.Join(home, ".codex", "state_5.sqlite")
	var rows []map[string]string
	if cwd != "" && cwd != "?" {
		rows = querySQLiteMaps(db,
			`SELECT tokens_used, title, IFNULL(model,'') AS model FROM threads WHERE archived=0 AND cwd=? ORDER BY updated_at DESC LIMIT 1`, cwd)
	} else if runtime.GOOS == "windows" {
		// Windows does not make another process's current directory available.
		// Fall back to the latest active thread so a normal Codex invocation
		// still shows its local context use; --recent lists all active threads.
		rows = querySQLiteMaps(db,
			`SELECT tokens_used, title, IFNULL(model,'') AS model FROM threads WHERE archived=0 ORDER BY updated_at DESC LIMIT 1`)
	}
	model := firstFlagValue(cmd, "-m", "--model")
	if len(rows) > 0 {
		row := rows[0]
		if t := parseTok(row["tokens_used"]); t != nil {
			s.Tokens = t
		}
		if row["model"] != "" {
			model = row["model"]
		}
		if t := tidyTitle(row["title"]); t != "" {
			s.Title = t
		}
	}
	if model != "" {
		s.Model = model
	}
	if s.Tokens != nil {
		s.Ctx = ctxPct(*s.Tokens, codexWindow(model, windows))
	}
}

func recentCodexIdle(home string, existing []Session, window time.Duration, windows map[string]int64) []Session {
	seen := seenDirs(existing, "codex", true)
	cutoff := time.Now().Add(-window).Unix()
	rows := querySQLiteMaps(filepath.Join(home, ".codex", "state_5.sqlite"),
		`SELECT tokens_used, title, cwd, IFNULL(model,'') AS model FROM threads WHERE archived=0 AND updated_at>=? ORDER BY updated_at DESC LIMIT 8`,
		strconv.FormatInt(cutoff, 10))
	var out []Session
	for _, row := range rows {
		dir := shortPath(row["cwd"])
		if dir == "?" || seen[dir] {
			continue
		}
		s := idleSession("codex", dir)
		s.Title = tidyTitle(row["title"])
		s.Model = row["model"]
		if t := parseTok(row["tokens_used"]); t != nil {
			s.Tokens = t
			s.Ctx = ctxPct(*t, codexWindow(row["model"], windows))
		}
		out = append(out, s)
		seen[dir] = true
	}
	return out
}

func seenDirs(existing []Session, agent string, liveOnly bool) map[string]bool {
	seen := map[string]bool{}
	for _, r := range existing {
		if r.Agent != agent {
			continue
		}
		if liveOnly && !r.Live {
			continue
		}
		seen[r.Dir] = true
	}
	return seen
}

func idleSession(agent, dir string) Session {
	return Session{Live: false, Status: "idle", Agent: agent, Dir: dir}
}

func parseTok(s string) *float64 {
	t, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &t
}

func shortPath(p string) string {
	if p == "" || p == "?" {
		return "?"
	}
	p = strings.TrimRight(p, "/\\")
	if home, _ := os.UserHomeDir(); home != "" {
		normP := strings.ReplaceAll(p, "\\", "/")
		normHome := strings.TrimRight(strings.ReplaceAll(home, "\\", "/"), "/")
		if strings.EqualFold(normP, normHome) || strings.HasPrefix(strings.ToLower(normP), strings.ToLower(normHome+"/")) {
			p = "~" + normP[len(normHome):]
		}
	}
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 && i+1 < len(p) {
		p = p[i+1:]
	}
	return SanitizeDisplay(p)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func tidyTitle(s string) string {
	s = firstLine(s)
	if strings.HasPrefix(s, "schema_version:") {
		return ""
	}
	return SanitizeDisplay(s)
}

func codexWindow(model string, windows map[string]int64) int64 {
	if w := windows[model]; w > 0 {
		return w
	}
	// Live exec often names the model; cache miss still needs a window
	// so CTX is not blank when tokens are known.
	if strings.HasPrefix(model, "gpt-5") || model == "" {
		return 272000 * 95 / 100
	}
	return 0
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
