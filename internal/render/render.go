package render

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/screenleon/agent-usage/internal/collect"
	"github.com/screenleon/agent-usage/internal/quota"
)

func Snapshot(w io.Writer, snap collect.Snapshot, q *quota.Report, interval time.Duration) {
	fmt.Fprintf(w, "agent-usage  %s\n\n", snap.Taken.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "SESSIONS\n")
	fmt.Fprintf(w, "%-5s %-8s %-7s %6s %6s %5s %7s  %s\n",
		"ST", "AGENT", "PID", "CPU", "MEM", "CTX", "TOKENS", "DIR / TITLE")
	fmt.Fprintf(w, "%-5s %-8s %-7s %6s %6s %5s %7s  %s\n",
		"--", "-----", "---", "---", "---", "---", "------", "----------")
	if len(snap.Sessions) == 0 {
		fmt.Fprintln(w, "(no live Claude / Grok / Codex / OpenCode session)")
	} else {
		for _, s := range sortSessions(snap.Sessions) {
			pid := "-"
			if s.PID > 0 {
				pid = strconv.Itoa(s.PID)
			}
			cpu := "-"
			if s.Live && s.CPU > 0 {
				cpu = fmt.Sprintf("%.1f%%", s.CPU)
			}
			mem := "-"
			if s.Live && s.RSSKB > 0 {
				mem = fmtRSS(s.RSSKB)
			}
			ctx := s.Ctx
			if ctx == "" {
				ctx = "-"
			}
			tok := "-"
			if s.Tokens != nil {
				tok = fmtTok(*s.Tokens)
			}
			loc := collect.SanitizeDisplay(s.Dir)
			if s.Title != "" && s.Title != s.Dir {
				loc = loc + " · " + TruncTitle(s.Title, 40)
			}
			if s.Kids > 0 {
				loc += fmt.Sprintf(" +%d", s.Kids)
			}
			fmt.Fprintf(w, "%-5s %-8s %-7s %6s %6s %5s %7s  %s\n",
				s.Status, s.Agent, pid, cpu, mem, ctx, tok, loc)
		}
	}
	if q != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "QUOTA")
		writeClaude(w, q.Claude)
		writeGrok(w, q.Grok)
		writeCodex(w, q.Codex)
	}
	if interval > 0 {
		fmt.Fprintf(w, "\nrefresh %s  ·  Ctrl-C to stop\n", interval)
	}
}

func writeClaude(w io.Writer, c quota.Claude) {
	if !c.OK {
		if c.Error != "" {
			fmt.Fprintf(w, "  claude  %s\n", c.Error)
		}
		return
	}
	u5, r5 := pctPair(c.Used5h)
	u7, _ := pctPair(c.Used7d)
	age := "?"
	if c.Updated > 0 {
		age = strconv.FormatInt((time.Now().Unix()-c.Updated)/60, 10) + "min ago"
	}
	fmt.Fprintf(w, "  claude  5h used %s remaining %s  reset %s  (%s)\n",
		u5, r5, fmtUnixClock(c.Reset5h), age)
	fmt.Fprintf(w, "          7d used %s\n", u7)
}

func writeGrok(w io.Writer, g quota.Grok) {
	if !g.OK {
		fmt.Fprintf(w, "  grok    %s\n", or(g.Error, "unavailable"))
		return
	}
	u, r := pctPair(g.Used)
	end := g.End
	if t, err := time.Parse(time.RFC3339Nano, g.End); err == nil {
		end = t.Local().Format("01-02 15:04")
	} else if t, err := time.Parse(time.RFC3339, g.End); err == nil {
		end = t.Local().Format("01-02 15:04")
	}
	fmt.Fprintf(w, "  grok    week used %s remaining %s  reset %s\n", u, r, end)
	for _, p := range g.Products {
		fmt.Fprintf(w, "          %s %s used\n", p.Name, fmtPct(p.Used))
	}
}

func writeCodex(w io.Writer, c quota.Codex) {
	if !c.OK {
		fmt.Fprintf(w, "  codex   %s\n", or(c.Error, "unavailable"))
		return
	}
	plan := c.Plan
	if plan == "" {
		plan = "?"
	}
	if c.Primary == nil || c.Primary.Used == nil {
		fmt.Fprintf(w, "  codex   %-7s (no window)\n", plan)
		return
	}
	fmt.Fprintf(w, "  codex   %-7s %s used %s remaining %s  reset in %s\n",
		plan, winLabel(c.Primary.WindowSeconds),
		fmtPct(*c.Primary.Used), fmtPct(*c.Primary.Remaining),
		fmtDur(c.Primary.ResetAfter))
	for _, ex := range c.Extra {
		if ex.Primary == nil || ex.Primary.Used == nil {
			continue
		}
		fmt.Fprintf(w, "          %-16s %s used %s remaining %s  reset in %s\n",
			ex.Name, winLabel(ex.Primary.WindowSeconds),
			fmtPct(*ex.Primary.Used), fmtPct(*ex.Primary.Remaining),
			fmtDur(ex.Primary.ResetAfter))
	}
}

func pctPair(used *float64) (string, string) {
	if used == nil {
		return "?", "?"
	}
	return fmtPct(*used), fmtPct(quota.Remaining(*used))
}

func fmtPct(v float64) string {
	return fmt.Sprintf("%.0f%%", v)
}

func fmtRSS(kb uint64) string {
	switch {
	case kb >= 1048576:
		return fmt.Sprintf("%.1fG", float64(kb)/1048576)
	case kb >= 1024:
		return fmt.Sprintf("%.0fM", float64(kb)/1024)
	default:
		return fmt.Sprintf("%dK", kb)
	}
}

func fmtTok(n float64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", n/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%.0fk", n/1000)
	default:
		return strconv.FormatInt(int64(n), 10)
	}
}

func fmtUnixClock(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("15:04")
}

func fmtDur(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func winLabel(sec int64) string {
	switch sec {
	case 18000:
		return "5h"
	case 86400:
		return "1d"
	case 604800:
		return "7d"
	default:
		if sec > 0 {
			return fmtDur(sec)
		}
		return "window"
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func sortSessions(in []collect.Session) []collect.Session {
	out := append([]collect.Session(nil), in...)
	rank := map[string]int{"busy": 0, "run": 1, "idle": 2}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rank[out[i].Status], rank[out[j].Status]
		if ri != rj {
			return ri < rj
		}
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		return out[i].PID < out[j].PID
	})
	return out
}

func TruncTitle(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = collect.SanitizeDisplay(s)
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
