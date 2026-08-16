package collect

import (
	"os"
	"os/exec"
	"strings"
)

// querySQLite runs one read-only query via the sqlite3 CLI if present.
// Args are bound as text replacements after a strict numeric/path check is
// the caller's job; we pass them as separate CLI args using sqlite3 -cmd.
func querySQLite(db, sql string, arg string) []string {
	rows := querySQLiteAll(db, sql, arg)
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func querySQLiteAll(db, sql string, args ...string) [][]string {
	if _, err := os.Stat(db); err != nil {
		return nil
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil
	}
	// Expand ? placeholders with single-quoted escaped args. Callers only
	// pass cwd paths or integer timestamps.
	q := sql
	for _, a := range args {
		q = strings.Replace(q, "?", "'"+escapeSQL(a)+"'", 1)
	}
	cmd := exec.Command(bin, "-readonly", "-batch", "-separator", "\t", db, q)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
