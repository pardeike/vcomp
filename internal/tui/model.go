package tui

import (
	"path/filepath"
	"unicode/utf8"

	"vcomp/internal/bootstrap"
	"vcomp/internal/engine"
)

var screens = []string{"Dashboard", "Public tests", "Product", "Settings", "Goal / result", "Activity", "Roles"}

type data struct {
	View                                       engine.Observation
	Product, Diff, Goal, Result, Log, Settings string
	ProductSummary                             string
	AgentDocs                                  map[string][]string
	RunDocs                                    map[string]string
	Positions                                  []bootstrap.Position
}
type field struct {
	Label, Value string
	Choices      []string
}
type form struct {
	Kind, Title      string
	Fields           []field
	Selected, Cursor int
	Editing          bool
}
type action struct {
	Kind   string
	Values []string
}
type model struct {
	Root                          string
	Screen, Selected, Scroll, Sub int
	Detail                        string
	Form                          *form
	Confirm                       *action
	Confirmation                  string
	Data                          data
	Message                       string
	Busy, OwnEngine, Follow       bool
	W, H                          int
}

func (m *model) count() int {
	switch m.Screen {
	case 0:
		return len(m.Data.View.Agents)
	case 1:
		return len(m.Data.View.Runs)
	case 6:
		return len(m.Data.Positions)
	}
	return 0
}
func (m *model) agent() string {
	if m.Selected >= 0 && m.Selected < len(m.Data.View.Agents) {
		return m.Data.View.Agents[m.Selected].Name
	}
	return ""
}
func (m *model) run() string {
	if m.Selected >= 0 && m.Selected < len(m.Data.View.Runs) {
		return m.Data.View.Runs[m.Selected].Name
	}
	return ""
}
func (m *model) update(d data) {
	name := ""
	if m.Screen == 0 {
		name = m.agent()
	} else if m.Screen == 1 {
		name = m.run()
	}
	m.Data = d
	if name != "" {
		if m.Screen == 0 {
			for i, a := range d.View.Agents {
				if a.Name == name {
					m.Selected = i
				}
			}
		} else {
			for i, r := range d.View.Runs {
				if r.Name == name {
					m.Selected = i
				}
			}
		}
	}
	m.Selected = max(0, min(m.Selected, m.count()-1))
}
func (m *model) switchScreen(n int) {
	m.Follow = n == 5
	m.Screen = n
	m.Selected = 0
	m.Scroll = 0
	m.Sub = 0
	m.Detail = ""
	m.Message = ""
}
func (m *model) key(k key) *action {
	if k.Name == "eof" || k.Name == "ctrl-c" || k.Name == "ctrl-d" {
		return &action{Kind: "quit"}
	}
	if m.Busy {
		return nil
	}
	if m.Confirm != nil {
		if k.Text == "y" || k.Text == "Y" {
			a := m.Confirm
			m.Confirm = nil
			return a
		}
		if k.Text == "n" || k.Name == "esc" {
			m.Confirm = nil
			m.Message = "Cancelled."
		}
		return nil
	}
	if m.Form != nil {
		return m.formKey(k)
	}
	if k.Text == "q" {
		if m.OwnEngine {
			m.Confirm = &action{Kind: "quit"}
			m.Confirmation = "Leave this run? Its supervisor will stop. Agents stay in tmux and keep working unsupervised."
			return nil
		}
		return &action{Kind: "quit"}
	}
	if k.Name == "esc" {
		m.Detail = ""
		m.Scroll = 0
		return nil
	}
	if k.Text == "?" {
		m.Detail = "help"
		m.Scroll = 0
		return nil
	}
	if k.Name == "tab" || k.Name == "backtab" {
		if m.Detail == "agent" {
			step := 1
			if k.Name == "backtab" {
				step = -1
			}
			m.Sub = (m.Sub + step + 5) % 5
			m.Scroll = 0
			return nil
		}
		step := 1
		if k.Name == "backtab" {
			step = -1
		}
		m.switchScreen((m.Screen + step + len(screens)) % len(screens))
		return nil
	}
	if len(k.Text) == 1 && k.Text[0] >= '1' && k.Text[0] <= '7' {
		m.switchScreen(int(k.Text[0] - '1'))
		return nil
	}
	delta := 0
	switch {
	case k.Name == "up" || k.Text == "k":
		delta = -1
	case k.Name == "down" || k.Text == "j":
		delta = 1
	case k.Name == "pgup":
		delta = -max(1, m.H-10)
	case k.Name == "pgdown":
		delta = max(1, m.H-10)
	}
	if delta != 0 {
		m.Follow = false
		if m.Detail != "" || m.count() == 0 {
			m.Scroll = max(0, m.Scroll+delta)
		} else {
			m.Selected = max(0, min(m.count()-1, m.Selected+delta))
		}
		return nil
	}
	if k.Name == "home" {
		m.Follow = false
		m.Scroll = 0
		if m.Detail == "" {
			m.Selected = 0
		}
		return nil
	}
	if k.Name == "end" {
		m.Follow = false
		if m.count() > 0 && m.Detail == "" {
			m.Selected = m.count() - 1
		} else {
			m.Scroll = 1 << 30
		}
		return nil
	}
	if k.Text == "f" {
		m.Follow = true
		m.Scroll = 0
		return nil
	}
	if k.Name == "enter" {
		switch m.Screen {
		case 0:
			if m.agent() != "" {
				m.Detail = "agent"
				m.Sub = 0
				m.Scroll = 0
			}
		case 1:
			if m.run() != "" {
				m.Detail = "run"
				m.Scroll = 0
			}
		case 2:
			m.Detail = "diff"
			m.Scroll = 0
		case 3:
			return &action{Kind: "settings-form"}
		case 6:
			return &action{Kind: "hire-form"}
		}
		return nil
	}
	switch k.Text {
	case "s":
		return &action{Kind: "start"}
	case "x":
		m.Confirm = &action{Kind: "stop"}
		m.Confirmation = "Stop this company's engine and all its agent sessions? Files and conversation history are kept."
	case "r":
		return &action{Kind: "reset-confirm"}
	case "h":
		return &action{Kind: "hire-form"}
	case "t":
		if m.Screen == 0 && m.agent() != "" {
			return &action{Kind: "steer-form", Values: []string{m.agent()}}
		}
	case "p":
		if m.Screen == 0 && m.agent() != "" {
			return &action{Kind: "role-settings-form", Values: []string{m.agent()}}
		}
	case "b":
		if m.Screen == 0 && m.agent() != "" {
			return &action{Kind: "replace-form", Values: []string{m.agent()}}
		}
	case "n":
		return &action{Kind: "test-form"}
	case "c":
		return &action{Kind: "settings-form"}
	case "o":
		m.Form = &form{Kind: "open", Title: "Open a company", Fields: []field{{Label: "Company directory", Value: m.Root}}}
	case "e":
		return &action{Kind: "editor"}
	case "a":
		if m.Screen == 0 && m.agent() != "" {
			return &action{Kind: "attach", Values: []string{m.Data.View.Agents[m.Selected].Session}}
		}
	}
	return nil
}
func (m *model) formKey(k key) *action {
	f := m.Form
	if k.Name == "esc" {
		if f.Editing {
			f.Editing = false
		} else {
			m.Form = nil
		}
		return nil
	}
	if k.Name == "save" {
		values := []string{}
		for _, v := range f.Fields {
			values = append(values, v.Value)
		}
		return &action{Kind: f.Kind, Values: values}
	}
	if k.Name == "tab" || k.Name == "backtab" {
		step := 1
		if k.Name == "backtab" {
			step = -1
		}
		f.Selected = (f.Selected + step + len(f.Fields)) % len(f.Fields)
		f.Editing = false
		return nil
	}
	v := &f.Fields[f.Selected]
	if k.Name == "enter" {
		f.Editing = !f.Editing
		f.Cursor = utf8.RuneCountInString(v.Value)
		return nil
	}
	if !f.Editing {
		if k.Name == "up" {
			f.Selected = max(0, f.Selected-1)
		}
		if k.Name == "down" {
			f.Selected = min(len(f.Fields)-1, f.Selected+1)
		}
		if (k.Name == "left" || k.Name == "right") && len(v.Choices) > 0 {
			i := 0
			for n, s := range v.Choices {
				if s == v.Value {
					i = n
				}
			}
			step := 1
			if k.Name == "left" {
				step = -1
			}
			v.Value = v.Choices[(i+step+len(v.Choices))%len(v.Choices)]
		}
		if k.Text != "" || k.Name == "paste" {
			f.Editing = true
			f.Cursor = utf8.RuneCountInString(v.Value)
		} else {
			return nil
		}
	}
	rs := []rune(v.Value)
	f.Cursor = max(0, min(f.Cursor, len(rs)))
	switch k.Name {
	case "left":
		f.Cursor = max(0, f.Cursor-1)
	case "right":
		f.Cursor = min(len(rs), f.Cursor+1)
	case "home":
		f.Cursor = 0
	case "end":
		f.Cursor = len(rs)
	case "clear":
		v.Value = ""
		f.Cursor = 0
	case "backspace":
		if f.Cursor > 0 {
			v.Value = string(append(rs[:f.Cursor-1], rs[f.Cursor:]...))
			f.Cursor--
		}
	case "delete":
		if f.Cursor < len(rs) {
			v.Value = string(append(rs[:f.Cursor], rs[f.Cursor+1:]...))
		}
	}
	if k.Text != "" {
		txt := []rune(k.Text)
		next := append([]rune{}, rs[:f.Cursor]...)
		next = append(next, txt...)
		next = append(next, rs[f.Cursor:]...)
		v.Value = string(next)
		f.Cursor += len(txt)
	}
	return nil
}
func (m *model) status() string {
	v := m.Data.View
	if v.Finished {
		return "FINISHED"
	}
	if v.Supervised {
		return "RUNNING / UPDATING"
	}
	for _, a := range v.Agents {
		if a.Session != "" && a.State != "exited" {
			return "UNSUPERVISED AGENTS"
		}
	}
	if !v.Exists {
		return "NOT SET UP"
	}
	return "STOPPED"
}
func (m *model) document() string {
	switch m.Detail {
	case "help":
		return helpText
	case "agent":
		a := m.agent()
		if docs := m.Data.AgentDocs[a]; len(docs) > m.Sub {
			return docs[m.Sub]
		}
		return "No document available."
	case "run":
		return m.Data.RunDocs[m.run()]
	case "diff":
		return m.Data.Diff
	}
	switch m.Screen {
	case 2:
		return m.Data.Product
	case 3:
		return m.Data.Settings
	case 4:
		return "GOAL\n\n" + m.Data.Goal + "\n\nRESULT\n\n" + m.Data.Result
	case 5:
		return m.Data.Log
	}
	return ""
}
func (m *model) title() string {
	if m.Detail == "agent" {
		return m.agent() + " / " + []string{"Terminal", "Inbox", "Notes", "Personal goals", "Role"}[m.Sub]
	}
	if m.Detail == "run" {
		return m.run()
	}
	if m.Detail == "diff" {
		return "Product diff"
	}
	if m.Detail == "help" {
		return "Keyboard help"
	}
	return screens[m.Screen]
}
func (m *model) rootLabel() string {
	return filepath.Base(m.Root)
}

const helpText = `NAVIGATION
Tab / Shift-Tab  Change screen; in agent detail, change document
1-7              Go directly to a screen
Up/Down or j/k   Select an item or scroll a document
PageUp/PageDown  Move a page
Home/End         First/last item or top/bottom of a document
Enter            Inspect an item; edit settings; hire from catalogue
Esc              Back / cancel
f                Return the activity view to its latest lines

COMPANY ACTIONS
s   Start or resume supervision (or view the existing engine)
x   Stop this company's engine and agents, after confirmation
r   Reset this company's output, after confirmation
c   Set up a company / change common settings
o   Open a different company directory
h   Hire an employee
n   Create a public user test

SELECTED EMPLOYEE
 a   Watch its tmux session; detach with Ctrl-B then d
     Inside tmux: Ctrl-B then L returns to the dashboard
 t   Edit CEO steering for this employee
 b   Replace its backstory, retaining profession and steering
 p   Set its harness, model, effort, and nudge pacing

FILES AND FORMS
 e   Open the settings file, goal, or selected document in $EDITOR
     The default editor is vi. Generated role.md is read-only here.
 Tab / Shift-Tab   Change field
 Enter            Edit field / finish editing
 Left/Right       Cycle choices, or move the text cursor
 Ctrl-U           Clear the field being edited
 Ctrl-S           Validate and save the form
 Esc              Finish editing / cancel form

EXIT
q   Leave the TUI. If it started a supervisor, confirmation explains
    that the supervisor stops while agents continue unsupervised.
Ctrl-C / Ctrl-D   Exit immediately, restoring the terminal.

STATE LABELS
live means a session exists; it does not prove the model is thinking.
quiet is a count of unchanged pane observations at engine ticks.
starting, error, and broken come from timestamped engine telemetry.
Terminal previews show actual captured output, not inferred progress.

CLI
Use --plain to retain command-line behavior. Redirected input/output
also uses the CLI. Settings, scripts, and role instructions still work.
`
