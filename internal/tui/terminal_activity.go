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
	if a.Error != "" {
		header += "Error: " + a.Error + "\n"
	}
	if mode == "raw" {
		return header + "\n" + strings.TrimSpace(a.Output)
	}
	if !a.Turns.Known {
		return header + "Recorded activity unavailable for this session. Raw terminal follows.\n\n" + strings.TrimSpace(a.Output)
	}
	if !a.Turns.Started.IsZero() {
		header += fmt.Sprintf("Current turn started %s · elapsed %s\n", a.Turns.Started.Local().Format("15:04:05"), time.Since(a.Turns.Started).Round(time.Second))
	}
	if status := ompLiveLine(a.Output); status != "" {
		header += "Live terminal: " + status + "\n"
	}
	header += "Recorded activity, newest first. In-flight generation may not be recorded yet.\n"
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
	return header + "\n" + activity
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
