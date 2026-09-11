package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"vcomp/internal/engine"
)

func terminalActivity(a engine.AgentView, mode string) string {
	if mode == "" {
		mode = "brief"
	}
	header := "Terminal · " + mode + " · read-only · v changes verbosity\n"
	if a.DirectPending > 0 {
		header += fmt.Sprintf("Direct steering: %d queued prompts waiting for this conversation\n", a.DirectPending)
	}
	if a.Error != "" {
		header += "Error: " + a.Error + "\n"
	}
	if mode == "raw" {
		return header + documentSection("RAW TERMINAL", a.Output)
	}
	if !a.Turns.Known {
		return header + "Recorded activity unavailable for this session.\n" + documentSection("RAW TERMINAL", a.Output)
	}
	if !a.Turns.Started.IsZero() {
		header += fmt.Sprintf("Current turn started %s · elapsed %s\n", a.Turns.Started.Local().Format("15:04:05"), time.Since(a.Turns.Started).Round(time.Second))
	}
	if status := ompLiveLine(a.Output); status != "" {
		header += "Live terminal: " + status + "\n"
	}
	header += "Recorded activity, oldest to newest. In-flight generation may not be recorded yet.\n"
	if !a.Turns.Latest.IsZero() {
		header += "Latest record " + time.Since(a.Turns.Latest).Round(time.Second).String() + " ago\n"
	}
	activity := a.Turns.Activity
	if mode == "detailed" {
		activity = a.Turns.Detail
	}
	if activity == "" {
		activity = "No recorded messages yet."
	}
	return header + documentSection("RECORDED ACTIVITY", activity)
}

// OMP puts a short current-operation line above its elapsed/model status row.
// Show its literal wording as terminal output, never as an inferred activity.
func ompLiveLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i > 0; i-- {
		if strings.Contains(lines[i], " > ") {
			for j := i - 1; j >= 0; j-- {
				line := strings.TrimSpace(lines[j])
				if line == "" {
					continue
				}
				return strings.TrimLeftFunc(line, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
			}
		}
	}
	return ""
}

func (m *model) pauseTerminal() {
	if m.Detail == "agent" && m.Sub == 2 && m.TerminalSnapshot == "" && m.Selected < len(m.Data.View.Agents) {
		m.TerminalSnapshot = terminalActivity(m.Data.View.Agents[m.Selected], m.Data.View.Config.TerminalView)
	}
}

// Prefer conversation records over terminal chrome. This is the latest observed
// activity, not a claim that the agent is still performing that operation.
func dashboardActivity(a engine.AgentView) string {
	text := a.Turns.Activity
	if strings.TrimSpace(text) == "" {
		if a.Harness == "omp" {
			text = ompLiveLine(clean(a.Output))
		} else {
			text = clean(a.Output)
		}
	}
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.IndexFunc(line, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			if a.Turns.Activity != "" && len(line) > 10 && line[8:10] == "  " {
				if _, err := time.Parse("15:04:05", line[:8]); err == nil {
					line = line[10:]
				}
			}
			return line
		}
	}
	return "—"
}
