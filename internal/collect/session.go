package collect

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
}

func Collect(opt Options) Snapshot {
	if opt.Home == "" {
		opt.Home = os.Getenv("HOME")
	}
	if opt.RecentFor == 0 {
		opt.RecentFor = 2 * time.Hour
	}
	procs := liveAgentProcs()
	var rows []Session

	used := map[int]bool{}
	rows = append(rows, claudeSessions(opt.Home, procs, used)...)
	rows = append(rows, grokSessions(opt.Home, procs, used, opt.Recent)...)
	for pid, p := range procs {
		if used[pid] {
			continue
		}
		s := sessionFromProc(p, "run")
		if p.Agent == "codex" {
			enrichCodex(&s, opt.Home, p.CWD)
		}
		rows = append(rows, s)
		used[pid] = true
	}
	if opt.Recent {
		rows = append(rows, recentCodexIdle(opt.Home, rows, opt.RecentFor)...)
	}
	return Snapshot{Taken: time.Now(), Sessions: rows}
}

func sessionFromProc(p Proc, st string) Session {
	return Session{
		Live:    true,
		Status:  st,
		Agent:   p.Agent,
		PID:     p.PID,
		CPU:     p.CPU,
		RSSKB:   p.RSSKB,
		Elapsed: p.Elapsed,
		Kids:    p.Kids,
		Dir:     shortPath(p.CWD),
	}
}

func claudeSessions(home string, procs map[int]Proc, used map[int]bool) []Session {
	dir := filepath.Join(claudeHome(home), "sessions")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Session
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		if !pidAliveAgent(pid, "claude") {
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
		if meta.PID != 0 && meta.PID != pid {
			pid = meta.PID
			if !pidAliveAgent(pid, "claude") {
				continue
			}
		}
		p := procs[pid]
		st := meta.Status
		if st == "" {
			st = "idle"
		}
		s := sessionFromProc(p, st)
		if s.PID == 0 {
			s.PID = pid
			s.Agent = "claude"
			s.Live = true
		}
		if meta.CWD != "" {
			s.Dir = shortPath(meta.CWD)
		}
		s.Title = meta.Name
		s.Tokens = claudeTailTokens(home, meta.SessionID)
		out = append(out, s)
		used[pid] = true
	}
	return out
}

type claudeSessionFile struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Status    string `json:"status"`
	Name      string `json:"name"`
}

func claudeHome(home string) string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(home, ".claude")
}

func claudeTailTokens(home, sid string) *float64 {
	if sid == "" {
		return nil
	}
	root := filepath.Join(claudeHome(home), "projects")
	matches, _ := filepath.Glob(filepath.Join(root, "*", sid+".jsonl"))
	if len(matches) == 0 {
		return nil
	}
	return lastUsageTokens(matches[0])
}

func lastUsageTokens(path string) *float64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	const tail = 8192
	start := st.Size() - tail
	if start < 0 {
		start = 0
	}
	buf := make([]byte, st.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && len(buf) == 0 {
		return nil
	}
	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		var o struct {
			Message *struct {
				Usage *struct {
					Input         float64 `json:"input_tokens"`
					CacheRead     float64 `json:"cache_read_input_tokens"`
					CacheCreation float64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage *struct {
				Input         float64 `json:"input_tokens"`
				CacheRead     float64 `json:"cache_read_input_tokens"`
				CacheCreation float64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(ln), &o) != nil {
			continue
		}
		var u *struct {
			Input         float64 `json:"input_tokens"`
			CacheRead     float64 `json:"cache_read_input_tokens"`
			CacheCreation float64 `json:"cache_creation_input_tokens"`
		}
		if o.Message != nil && o.Message.Usage != nil {
			u = o.Message.Usage
		} else if o.Usage != nil {
			u = o.Usage
		}
		if u == nil {
			continue
		}
		sum := u.Input + u.CacheRead + u.CacheCreation
		return &sum
	}
	return nil
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
			if p.CPU >= 3 {
				st = "busy"
			}
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
		if it.CWD != "" {
			s.Dir = shortPath(it.CWD)
		}
		if dir := grokSessionDir(home, it.CWD, it.SessionID); dir != "" {
			if sig := readGrokSignals(dir); sig != nil {
				if sig.ContextTokensUsed > 0 {
					t := float64(sig.ContextTokensUsed)
					s.Tokens = &t
				}
				if sig.ContextWindowUsage != 0 {
					s.Ctx = strconv.Itoa(sig.ContextWindowUsage) + "%"
				}
			}
			if sm := readGrokSummary(dir); sm != nil {
				s.Title = firstNonEmpty(sm.GeneratedTitle, sm.SessionSummary)
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
	ContextWindowUsage int   `json:"contextWindowUsage"`
	ContextTokensUsed  int64 `json:"contextTokensUsed"`
}

func readGrokSignals(dir string) *grokSignals {
	b, err := os.ReadFile(filepath.Join(dir, "signals.json"))
	if err != nil {
		return nil
	}
	var s grokSignals
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

type grokSummary struct {
	GeneratedTitle string `json:"generated_title"`
	SessionSummary string `json:"session_summary"`
}

func readGrokSummary(dir string) *grokSummary {
	b, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		return nil
	}
	var s grokSummary
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

func enrichCodex(s *Session, home, cwd string) {
	row := querySQLite(filepath.Join(home, ".codex", "state_5.sqlite"),
		`SELECT tokens_used, title FROM threads WHERE archived=0 AND cwd=? ORDER BY updated_at DESC LIMIT 1`,
		cwd)
	if len(row) >= 2 {
		if t, err := strconv.ParseFloat(row[0], 64); err == nil {
			s.Tokens = &t
		}
		s.Title = firstLine(row[1])
	}
}

func recentCodexIdle(home string, existing []Session, window time.Duration) []Session {
	seen := map[string]bool{}
	for _, r := range existing {
		if r.Agent == "codex" && r.Live {
			seen[r.Dir] = true
		}
	}
	cutoff := time.Now().Add(-window).Unix()
	rows := querySQLiteAll(filepath.Join(home, ".codex", "state_5.sqlite"),
		`SELECT tokens_used, title, cwd FROM threads WHERE archived=0 AND updated_at>=? ORDER BY updated_at DESC LIMIT 8`,
		strconv.FormatInt(cutoff, 10))
	var out []Session
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}
		dir := shortPath(row[2])
		if seen[dir] {
			continue
		}
		s := Session{Live: false, Status: "idle", Agent: "codex", Dir: dir, Title: firstLine(row[1])}
		if t, err := strconv.ParseFloat(row[0], 64); err == nil {
			s.Tokens = &t
		}
		out = append(out, s)
		seen[dir] = true
	}
	return out
}

func shortPath(p string) string {
	if p == "" || p == "?" {
		return "?"
	}
	p = strings.TrimRight(p, "/")
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(p, home+"/") {
		p = "~" + p[len(home):]
	}
	if i := strings.LastIndex(p, "/"); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
