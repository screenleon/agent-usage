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
	if err != nil || len(out) == 0 {
		return nil
	}
	return parseSQLiteJSON(out)
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
