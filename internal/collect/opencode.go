package collect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func opencodeDB(home string) string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "opencode", "opencode.db")
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func enrichOpenCode(s *Session, home, cwd string) {
	if cwd == "" || cwd == "?" {
		return
	}
	rows := querySQLiteMaps(opencodeDB(home),
		`SELECT directory, title, IFNULL(model,'') AS model, tokens_input, tokens_cache_read FROM session WHERE directory=? AND (time_archived IS NULL OR time_archived=0) ORDER BY time_updated DESC LIMIT 1`,
		cwd)
	applyOpenCodeRow(s, rows)
}

func recentOpenCodeIdle(home string, existing []Session, window time.Duration) []Session {
	seen := seenDirs(existing, "opencode", false)
	cutoff := time.Now().Add(-window).UnixMilli()
	rows := querySQLiteMaps(opencodeDB(home),
		`SELECT directory, title, IFNULL(model,'') AS model, tokens_input, tokens_cache_read FROM session WHERE (time_archived IS NULL OR time_archived=0) AND time_updated>=? ORDER BY time_updated DESC LIMIT 8`,
		strconv.FormatInt(cutoff, 10))
	var out []Session
	for _, row := range rows {
		dir := shortPath(row["directory"])
		if dir == "?" || seen[dir] {
			continue
		}
		s := idleSession("opencode", dir)
		applyOpenCodeRow(&s, []map[string]string{row})
		out = append(out, s)
		seen[dir] = true
	}
	return out
}

func applyOpenCodeRow(s *Session, rows []map[string]string) {
	if len(rows) == 0 {
		return
	}
	row := rows[0]
	if t := tidyTitle(row["title"]); t != "" {
		s.Title = t
	}
	if m := parseOpenCodeModel(row["model"]); m != "" {
		s.Model = m
	}
	in := parseTok(row["tokens_input"])
	cr := parseTok(row["tokens_cache_read"])
	if in != nil || cr != nil {
		var n float64
		if in != nil {
			n += *in
		}
		if cr != nil {
			n += *cr
		}
		s.Tokens = &n
	}
}

func parseOpenCodeModel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s[0] != '{' {
		return s
	}
	var m struct {
		ID string `json:"id"`
	}
	if json.Unmarshal([]byte(s), &m) == nil && m.ID != "" {
		return m.ID
	}
	return ""
}
