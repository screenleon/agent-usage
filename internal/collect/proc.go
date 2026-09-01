package collect

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Proc struct {
	PID     int
	Agent   string
	CPU     float64
	RSSKB   uint64
	Elapsed string
	CWD     string
	Kids    int
	Cmd     string
	Raw     string
}

func classify(comm, cmdline string) string {
	switch comm {
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	case "opencode":
		return "opencode"
	}
	if strings.HasPrefix(comm, "grok") {
		return "grok"
	}
	if comm == "node" {
		if nodeWraps(cmdline, "claude") {
			return "claude"
		}
		if nodeWraps(cmdline, "opencode") {
			return "opencode"
		}
	}
	// comm can be a versioned binary; match the executable basename only.
	switch firstArgBase(cmdline) {
	case "codex":
		return "codex"
	case "claude":
		return "claude"
	case "opencode":
		return "opencode"
	}
	return ""
}

func nodeWraps(cmdline, name string) bool {
	cmdline = strings.ToLower(strings.ReplaceAll(cmdline, "\\", "/"))
	name = strings.ToLower(name)
	return strings.Contains(cmdline, "/bin/"+name) ||
		strings.Contains(cmdline, "/"+name+".") ||
		strings.Contains(cmdline, "/"+name+"-") ||
		strings.HasSuffix(strings.TrimSpace(cmdline), "/"+name)
}

func firstArgBase(cmdline string) string {
	args := argv(cmdline)
	if len(args) == 0 {
		return ""
	}
	return filepath.Base(args[0])
}

func argv(cmdline string) []string {
	if strings.Contains(cmdline, "\x00") {
		var out []string
		for _, p := range strings.Split(cmdline, "\x00") {
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return strings.Fields(cmdline)
}

// isSelfMonitor reports whether this process is the monitor itself.
// Only the executable name is checked — never the rest of argv, or a
// `codex --cd …/agent-usage` session is dropped.
func isSelfMonitor(comm, cmdline string) bool {
	if comm == "agent-usage" {
		return true
	}
	return firstArgBase(cmdline) == "agent-usage"
}

func resolveCWD(procCWD, cd string) string {
	if cd == "" {
		return procCWD
	}
	if filepath.IsAbs(cd) || strings.HasPrefix(cd, "/") {
		return cd
	}
	if procCWD == "" || procCWD == "?" {
		return cd
	}
	return filepath.Clean(filepath.Join(procCWD, cd))
}

func firstFlagValue(cmdline string, names ...string) string {
	for _, name := range names {
		if v := flagValue(cmdline, name); v != "" {
			return v
		}
	}
	return ""
}

func flagValue(cmdline, name string) string {
	args := argv(cmdline)
	prefix := name + "="
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(args[i], prefix) {
			return args[i][len(prefix):]
		}
	}
	return ""
}

func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func liveAgentProcs() map[int]Proc {
	if runtime.GOOS == "windows" {
		return liveAgentProcsWindows()
	}
	out := make(map[int]Proc)
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	uptime := readUptime()
	hz := clkTck()
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		base := filepath.Join("/proc", e.Name())
		comm := readFileTrim(filepath.Join(base, "comm"))
		cmd := cmdlineOf(pid)
		if isSelfMonitor(comm, cmd) {
			continue
		}
		agent := classify(comm, cmd)
		if agent == "" {
			continue
		}
		cwd := resolveCWD(readCWD(pid), firstFlagValue(cmd, "--cd", "-C"))
		p := Proc{
			PID:   pid,
			Agent: agent,
			CWD:   SanitizeDisplay(cwd),
			Cmd:   strings.TrimSpace(strings.ReplaceAll(cmd, "\x00", " ")),
			Raw:   cmd,
			Kids:  countChildren(pid),
		}
		p.RSSKB, p.CPU, p.Elapsed = statUsage(base, uptime, hz)
		out[pid] = p
	}
	return out
}

func readFileBytes(path string) []byte {
	b, _ := os.ReadFile(path)
	return b
}

func readCWD(pid int) string {
	p, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
	if err != nil {
		return "?"
	}
	return p
}

func commOf(pid int) string {
	return readFileTrim(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
}

func cmdlineOf(pid int) string {
	return string(readFileBytes(filepath.Join("/proc", strconv.Itoa(pid), "cmdline")))
}

func pidAliveAgent(pid int, want string) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		p, ok := liveAgentProcsWindows()[pid]
		return ok && p.Agent == want
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err != nil {
		return false
	}
	return classify(commOf(pid), cmdlineOf(pid)) == want
}

func childPIDs(pid int) []int {
	path := filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children")
	return parseChildPIDs(readFileTrim(path))
}

func parseChildPIDs(s string) []int {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []int
	for _, f := range strings.Fields(s) {
		c, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out
}

func countChildren(pid int) int {
	return len(childPIDs(pid))
}

func childCmdlines(pid int) []string {
	ids := childPIDs(pid)
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, c := range ids {
		out = append(out, cmdlineOf(c))
	}
	return out
}

func readUptime() float64 {
	f := strings.Fields(readFileTrim("/proc/uptime"))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

func clkTck() float64 {
	// Linux default; avoid cgo/unix just for this.
	return 100
}

func statUsage(base string, uptime, hz float64) (rssKB uint64, cpu float64, elapsed string) {
	st := readFileTrim(filepath.Join(base, "stat"))
	// comm is in parens and may contain spaces; split after last ')'
	i := strings.LastIndex(st, ")")
	if i < 0 || i+2 >= len(st) {
		return 0, 0, "-"
	}
	fields := strings.Fields(st[i+2:])
	// fields[0] is state; utime=12, stime=13, starttime=20, rss=22 (0-based after comm)
	if len(fields) < 23 {
		return 0, 0, "-"
	}
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	start, _ := strconv.ParseFloat(fields[19], 64)
	rssPages, _ := strconv.ParseUint(fields[21], 10, 64)
	rssKB = rssPages * 4
	life := uptime - start/hz
	if life > 0 && hz > 0 {
		cpu = 100 * (utime + stime) / hz / life
	}
	if life < 0 {
		life = 0
	}
	elapsed = formatElapsed(life)
	return rssKB, cpu, elapsed
}

func formatElapsed(sec float64) string {
	s := int(sec)
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	ss := s % 60
	switch {
	case d > 0:
		return strconv.Itoa(d) + "d" + pad2(h) + "h"
	case h > 0:
		return strconv.Itoa(h) + ":" + pad2(m) + ":" + pad2(ss)
	default:
		return pad2(m) + ":" + pad2(ss)
	}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
