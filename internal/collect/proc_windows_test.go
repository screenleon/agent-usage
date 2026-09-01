package collect

import (
	"encoding/json"
	"testing"
)

func TestWindowsProcessJSON(t *testing.T) {
	var p windowsProcess
	if err := json.Unmarshal([]byte(`{"ProcessId":42,"ParentProcessId":7,"Name":"codex.exe","CommandLine":"codex --cd C:\\work","WorkingSetSize":1048576}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.PID != 42 || p.ParentPID != 7 || p.workingSetBytes() != 1048576 {
		t.Fatalf("decoded %#v", p)
	}
	if classify("codex", p.CommandLine) != "codex" {
		t.Fatal("codex process was not classified")
	}
}
