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
	fmt.Fprintf(w, "%-5s %-8s %-7s %7s %6s %6s %5s %7s %-16s  %s\n",
		"ST", "AGENT", "PID", "AGE", "CPU", "MEM", "CTX", "TOKENS", "MODEL", "DIR / TITLE")
	fmt.Fprintf(w, "%-5s %-8s %-7s %7s %6s %6s %5s %7s %-16s  %s\n",
		"--", "-----", "---", "---", "---", "---", "---", "------", "-----", "----------")
	if len(snap.Sessions) == 0 {
		fmt.Fprintln(w, "(no live Claude / Grok / Codex / OpenCode session)")
	} else {
		for _, s := range sortSessions(snap.Sessions) {
			pid := "-"
			if s.PID > 0 {
				pid = strconv.Itoa(s.PID)
			}
			age := dash(s.Elapsed)
			cpu := "-"
			if s.Live && s.CPU > 0 {
				cpu = fmt.Sprintf("%.1f%%", s.CPU)
			}
			mem := "-"
			if s.Live && s.RSSKB > 0 {
				mem = fmtRSS(s.RSSKB)
			}
			ctx := dash(s.Ctx)
			tok := "-"
			if s.Tokens != nil {
				tok = fmtTok(*s.Tokens)
			}
			model := dash(TruncTitle(s.Model, 16))
			loc := collect.SanitizeDisplay(s.Dir)
			if s.Title != "" && s.Title != s.Dir {
				loc = loc + " · " + TruncTitle(s.Title, 40)
			}
			if s.Kids > 0 {
				loc += fmt.Sprintf(" +%d", s.Kids)
			}
			fmt.Fprintf(w, "%-5s %-8s %-7s %7s %6s %6s %5s %7s %-16s  %s\n",
				s.Status, s.Agent, pid, age, cpu, mem, ctx, tok, model, loc)
		}
	}
	if q != nil {
		fmt.Fprintln(w)
		fmt.Fprint(w, "QUOTA")
		if !q.FetchedAt.IsZero() {
			age := time.Since(q.FetchedAt).Truncate(time.Second)
			if age >= time.Second {
				fmt.Fprintf(w, "  (cached %s)", age)
			}
		}
		fmt.Fprintln(w)
		writeClaude(w, q.Claude)
		writeGrok(w, q.Grok)
		writeCodex(w, q.Codex)
	}
	if interval > 0 {
		fmt.Fprintf(w, "\nrefresh %s  ·  Ctrl-C to stop\n", interval)
	}
}

func writeClaude(w io.Writer, c quota.Claude) {
	if skipQuota(w, "  claude  ", c.OK, c.Error) {
		return
	}
	u5, r5 := pctPair(c.Used5h)
	u7, r7 := pctPair(c.Used7d)
	age := "?"
	if c.Updated > 0 {
		age = strconv.FormatInt((time.Now().Unix()-c.Updated)/60, 10) + "min ago"
	}
	fmt.Fprintf(w, "  claude  %s5h used %s remaining %s  reset %s  (%s)\n",
		warnUsed(c.Used5h), u5, r5, fmtUnixClock(c.Reset5h), age)
	fmt.Fprintf(w, "          %s7d used %s remaining %s  reset %s\n", warnUsed(c.Used7d), u7, r7, fmtUnixDateTime(c.Reset7d))
	for _, x := range c.Extra {
		if x.Used == nil {
			continue
		}
		ux, rx := pctPair(x.Used)
		fmt.Fprintf(w, "          %s%s used %s remaining %s  reset %s\n",
			warnUsed(x.Used), x.Name, ux, rx, fmtUnixDateTime(x.Reset))
	}
}

func writeGrok(w io.Writer, g quota.Grok) {
	if skipQuota(w, "  grok    ", g.OK, g.Error) {
		return
	}
	u, r := pctPair(g.Used)
	end := g.End
	if t, ok := quota.ParseTime(g.End); ok {
		end = t.Local().Format("01-02 15:04")
	}
	top := ""
	if len(g.Products) > 0 {
		best := g.Products[0]
		for _, p := range g.Products[1:] {
			if p.Used > best.Used {
				best = p
			}
		}
		top = "  (" + best.Name + " " + fmtPct(best.Used) + ")"
	}
	fmt.Fprintf(w, "  grok    %sweek used %s remaining %s  reset %s%s\n", warnUsed(g.Used), u, r, end, top)
	for _, p := range g.Products {
		rem := quota.Remaining(p.Used)
		fmt.Fprintf(w, "          %s%s %s used remaining %s\n", warnRem(rem), p.Name, fmtPct(p.Used), fmtPct(rem))
	}
}

func writeCodex(w io.Writer, c quota.Codex) {
	if skipQuota(w, "  codex   ", c.OK, c.Error) {
		return
	}
	plan := quota.PlanName(c.Plan)
	main := c.Primary
	if !winOK(main) {
		main = c.Secondary
	}
	if !winOK(main) {
		fmt.Fprintf(w, "  codex   %-7s (no window)\n", plan)
	} else {
		writeWin(w, "  codex   "+fmt.Sprintf("%-7s ", plan), main)
		if main == c.Primary {
			writeWin(w, "          ", c.Secondary)
		}
	}
	for _, ex := range c.Extra {
		name := quota.LimitName(ex.Name)
		writeWin(w, fmt.Sprintf("          %-16s ", name), ex.Primary)
		writeWin(w, "                   ", ex.Secondary)
	}
	if c.Resets > 0 {
		exp := fmtDurUnix(c.ResetExpiry)
		if exp == "-" {
			fmt.Fprintf(w, "          %d reset available\n", c.Resets)
		} else {
			fmt.Fprintf(w, "          %d reset available · expires %s\n", c.Resets, exp)
		}
	}
}

func skipQuota(w io.Writer, prefix string, ok bool, err string) bool {
	if ok {
		return false
	}
	if err != "" {
		fmt.Fprintf(w, "%s%s\n", prefix, err)
	}
	return true
}

func writeWin(w io.Writer, prefix string, win *quota.Window) {
	if !winOK(win) {
		return
	}
	fmt.Fprintf(w, "%s%s%s used %s remaining %s  reset in %s\n",
		prefix, warnRem(*win.Remaining), winLabel(win.WindowSeconds),
		fmtPct(*win.Used), fmtPct(*win.Remaining), fmtDur(win.ResetAfter))
}

func winOK(w *quota.Window) bool {
	return w != nil && w.Used != nil && w.Remaining != nil
}

func warnUsed(used *float64) string {
	if used == nil {
		return ""
	}
	return warnRem(quota.Remaining(*used))
}

func warnRem(rem float64) string {
	if rem < quota.LowWater {
		return "! "
	}
	return ""
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func fmtDurUnix(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	d := time.Until(time.Unix(ts, 0))
	if d < 0 {
		return "-"
	}
	return fmtDur(int64(d.Seconds()))
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

func fmtUnixDateTime(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	return time.Unix(ts, 0).Format("01-02 15:04")
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

func sortSessions(in []collect.Session) []collect.Session {
	out := append([]collect.Session(nil), in...)
	rank := map[string]int{"busy": 0, "run": 1, "wait": 2, "idle": 3}
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
