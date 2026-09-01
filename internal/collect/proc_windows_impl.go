package collect

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// liveAgentProcsWindows uses CIM rather than /proc. Windows does not expose a
// process working directory, but Codex's --cd flag is available in its command
// line and is enough to associate the usual CLI and exec sessions with their
// local thread record.
func liveAgentProcsWindows() map[int]Proc {
	out := make(map[int]Proc)
	b, err := windowsProcessOutput("Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,Name,CommandLine,WorkingSetSize | ConvertTo-Json -Compress")
	if err != nil || len(b) == 0 {
		// CIM may be denied by an enterprise policy. Get-Process still exposes
		// the direct CLI executables without elevation, albeit without argv/CWD.
		b, err = windowsProcessOutput("Get-Process | Select-Object Id,ProcessName,WorkingSet64 | ConvertTo-Json -Compress")
	}
	if err != nil || len(b) == 0 {
		return out
	}
	var rows []windowsProcess
	if json.Unmarshal(b, &rows) != nil {
		var one windowsProcess
		if json.Unmarshal(b, &one) != nil || one.PID == 0 {
			return out
		}
		rows = []windowsProcess{one}
	}
	children := make(map[int]int)
	for _, row := range rows {
		children[row.ParentPID]++
	}
	for _, row := range rows {
		comm := strings.TrimSuffix(strings.ToLower(row.Name), ".exe")
		if isSelfMonitor(comm, row.CommandLine) {
			continue
		}
		agent := classify(comm, row.CommandLine)
		if agent == "" {
			continue
		}
		cwd := resolveCWD("?", firstFlagValue(row.CommandLine, "--cd", "-C"))
		out[row.PID] = Proc{
			PID: row.PID, Agent: agent, CWD: SanitizeDisplay(cwd),
			Cmd: strings.TrimSpace(row.CommandLine), Raw: row.CommandLine,
			RSSKB: row.workingSetBytes() / 1024, Kids: children[row.PID], Elapsed: "-",
		}
	}
	return out
}

func windowsProcessOutput(script string) ([]byte, error) {
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
}

type windowsProcess struct {
	PID        int    `json:"ProcessId"`
	ParentPID  int    `json:"ParentProcessId"`
	Name       string `json:"Name"`
	CommandLine string `json:"CommandLine"`
	WorkingSet string `json:"WorkingSetSize"`
}

func (p *windowsProcess) UnmarshalJSON(b []byte) error {
	var v struct {
		PID          int             `json:"ProcessId"`
		ID           int             `json:"Id"`
		ParentPID    int             `json:"ParentProcessId"`
		Name         string          `json:"Name"`
		ProcessName  string          `json:"ProcessName"`
		CommandLine  string          `json:"CommandLine"`
		WorkingSet   json.RawMessage `json:"WorkingSetSize"`
		WorkingSet64 json.RawMessage `json:"WorkingSet64"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	p.PID, p.ParentPID, p.Name, p.CommandLine = v.PID, v.ParentPID, v.Name, v.CommandLine
	if p.PID == 0 {
		p.PID = v.ID
	}
	if p.Name == "" {
		p.Name = v.ProcessName
	}
	p.WorkingSet = strings.Trim(string(v.WorkingSet), "\"")
	if p.WorkingSet == "" || p.WorkingSet == "null" {
		p.WorkingSet = strings.Trim(string(v.WorkingSet64), "\"")
	}
	return nil
}

func (p windowsProcess) workingSetBytes() uint64 {
	v, _ := strconv.ParseUint(p.WorkingSet, 10, 64)
	return v
}
