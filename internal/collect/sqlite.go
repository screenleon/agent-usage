package collect

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func querySQLiteMaps(db, sql string, args ...string) []map[string]string {
	if _, err := os.Stat(db); err != nil {
		return nil
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil
	}
	q := sql
	for _, a := range args {
		q = strings.Replace(q, "?", "'"+escapeSQL(a)+"'", 1)
	}
	cmd := exec.Command(bin, "-readonly", "-batch", "-json", db, q)
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		if rows := parseSQLiteJSON(out); rows != nil {
			return rows
		}
	}
	// Older sqlite3 builds lack -json; flatten newlines in the query and
	// parse USV rows so titles cannot split columns.
	fb := exec.Command(bin, "-readonly", "-batch", "-header", "-separator", "\x1f", db, flattenSQLNewlines(q))
	out, err = fb.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	return parseSQLiteUSV(out)
}

func flattenSQLNewlines(sql string) string {
	// Keep the result column named title so maps stay keyed the same as -json.
	return strings.ReplaceAll(sql, ", title,",
		", replace(replace(title, char(10), ' '), char(13), ' ') AS title,")
}

func parseSQLiteUSV(out []byte) []map[string]string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return nil
	}
	heads := strings.Split(lines[0], "\x1f")
	var rows []map[string]string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\x1f")
		m := make(map[string]string, len(heads))
		for i, h := range heads {
			if i < len(cols) {
				m[h] = cols[i]
			} else {
				m[h] = ""
			}
		}
		rows = append(rows, m)
	}
	return rows
}

func parseSQLiteJSON(out []byte) []map[string]string {
	var raw []map[string]any
	if json.Unmarshal(out, &raw) != nil {
		return nil
	}
	rows := make([]map[string]string, 0, len(raw))
	for _, obj := range raw {
		m := make(map[string]string, len(obj))
		for k, v := range obj {
			switch t := v.(type) {
			case nil:
				m[k] = ""
			case string:
				m[k] = t
			case float64:
				m[k] = strconv.FormatFloat(t, 'f', -1, 64)
			case bool:
				if t {
					m[k] = "1"
				} else {
					m[k] = "0"
				}
			default:
				b, _ := json.Marshal(t)
				m[k] = string(b)
			}
		}
		rows = append(rows, m)
	}
	return rows
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
