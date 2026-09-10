package tui

import (
	"fmt"
	"strings"
	"time"
)

type row struct{ Text, Style string }

const accent = "\x1b[1;36m"
const selected = "\x1b[30;46m"
const dim = "\x1b[2m"
const warning = "\x1b[33m"

func (m *model) render(w, h int) []row {
	m.W, m.H = w, h
	if w < 40 || h < 12 {
		return fitRows([]row{{"vcomp", accent}, {"Terminal too small", warning}, {"Use at least 40 columns x 12 rows.", ""}, {"q or Ctrl-C to exit", dim}}, w, h)
	}
	rows := []row{{" vcomp  /  " + m.rootLabel(), accent}}
	nav := []string{}
	for i, s := range screens {
		if w < 100 {
			s = []string{"Home", "Tests", "Git", "Config", "Goal", "Log", "Roles"}[i]
		}
		if i == m.Screen {
			s = "[" + s + "]"
		}
		nav = append(nav, fmt.Sprintf("%d %s", i+1, s))
	}
	if w < 65 {
		rows = append(rows, row{fmt.Sprintf(" %d/%d  %s  | Tab: next screen", m.Screen+1, len(screens), m.title()), accent})
	} else {
		rows = append(rows, row{" " + strings.Join(nav, "  "), ""})
	}
	observed := "no engine observation"
	if !m.Data.View.ObservedAt.IsZero() {
		observed = "tick " + time.Since(m.Data.View.ObservedAt).Round(time.Second).String() + " ago"
	}
	rows = append(rows, row{" " + m.status() + "  |  " + observed, dim})
	contentH := h - 6
	body := []row{}
	switch {
	case m.Confirm != nil:
		body = append(body, row{" Confirm action", warning})
		for _, s := range wrap(m.Confirmation, w-2) {
			body = append(body, row{" " + s, ""})
		}
		body = append(body, row{"", ""}, row{" y Confirm    n / Esc Cancel", warning})
	case m.Form != nil:
		body = m.renderForm(w, contentH)
	case m.Detail != "" || m.count() == 0 && m.Screen != 0 && m.Screen != 1 && m.Screen != 6:
		body = append(body, row{" " + m.title(), accent})
		if m.Detail == "agent" {
			body = append(body, row{" Tab: Terminal / Inbox / Notes / Goals / Role", dim})
		}
		lines := wrap(m.document(), w-2)
		if len(lines) == 0 {
			lines = []string{"Nothing here yet."}
		}
		size := contentH - len(body)
		if m.Screen == 5 && m.Detail == "" && m.Follow {
			m.Scroll = max(0, len(lines)-size)
		}
		m.Scroll = max(0, min(m.Scroll, max(0, len(lines)-size)))
		for _, s := range lines[m.Scroll:min(len(lines), m.Scroll+size)] {
			body = append(body, row{" " + s, ""})
		}
	case m.Screen == 0:
		body = m.dashboard(w, contentH)
	case m.Screen == 1:
		body = append(body, row{" Public user tests    n: new test    Enter: read", accent})
		if m.count() == 0 {
			body = append(body, row{" No public tests yet. Press n to create one.", dim})
		}
		start := max(0, m.Selected-(contentH-3))
		for i := start; i < min(m.count(), start+contentH-1); i++ {
			r := m.Data.View.Runs[i]
			body = append(body, itemRow(i == m.Selected, fmt.Sprintf("%-14s %s", r.Name, r.State)))
		}
	case m.Screen == 6:
		body = append(body, row{" Professions    Enter: hire    h: custom name", accent})
		start := max(0, m.Selected-(contentH-3))
		for i := start; i < min(m.count(), start+contentH-1); i++ {
			p := m.Data.Positions[i]
			body = append(body, itemRow(i == m.Selected, fmt.Sprintf("%-23s %s", p.Name, p.Title)))
		}
	}
	for len(body) < contentH {
		body = append(body, row{})
	}
	if len(body) > contentH {
		body = body[:contentH]
	}
	rows = append(rows, body...)
	message := m.Message
	if m.Busy {
		message = "Working..."
	}
	if m.Data.View.Error != "" && message == "" {
		message = m.Data.View.Error
	}
	rows = append(rows, row{" " + message, warning})
	footer := " Enter inspect  Tab screens  s start  x stop  ? help  q leave"
	if w < 75 {
		footer = " Enter inspect  Tab screens  ? help  q leave"
	}
	if m.Form != nil {
		footer = " Tab field  Enter edit  Ctrl-S save  Esc cancel"
	}
	if m.Confirm != nil {
		footer = " y confirm   n / Esc cancel"
	}
	rows = append(rows, row{footer, dim}, row{" " + m.title() + "  |  " + fmt.Sprintf("%dx%d", w, h), dim})
	return fitRows(rows, w, h)
}
func itemRow(active bool, s string) row {
	if active {
		return row{" > " + s, selected}
	}
	return row{"   " + s, ""}
}
func (m *model) dashboard(w, h int) []row {
	live, inbox, done := 0, 0, 0
	for _, a := range m.Data.View.Agents {
		if a.Session != "" && a.State != "exited" {
			live++
		}
		inbox += a.Inbox
	}
	for _, r := range m.Data.View.Runs {
		if r.State == "impressions" {
			done++
		}
	}
	rows := []row{{fmt.Sprintf(" %d agents / %d sessions   %d inbox   tests %d/%d", len(m.Data.View.Agents), live, inbox, done, len(m.Data.View.Runs)), accent}}
	if len(m.Data.View.Agents) == 0 {
		return append(rows, row{" No company in this directory yet.", ""}, row{" c Set up a company    o Open a directory", accent}, row{" Setup only writes files when you save.", dim})
	}
	wide := w >= 120
	listW := w
	if wide {
		listW = w * 55 / 100
	}
	listH := h - 3
	if !wide && h >= 17 {
		listH = max(5, h*2/3)
	}
	header := "   " + pad("Agent", 18) + " " + pad("State", 11) + " " + pad("Inbox", 5)
	if listW >= 70 {
		header += "  " + pad("CLI", 10) + " " + pad("Idle", 4)
	}
	if listW >= 85 {
		header += "  Pane tail"
	}
	rows = append(rows, row{" Product: " + m.Data.ProductSummary, dim}, row{header, dim})
	start := max(0, m.Selected-listH+1)
	for i := start; i < min(m.count(), start+listH); i++ {
		a := m.Data.View.Agents[i]
		text := fmt.Sprintf("%s %s %5d", pad(a.Name, 18), pad(a.State, 11), a.Inbox)
		if listW >= 70 {
			text += fmt.Sprintf("  %-10s %-4d", a.Harness, a.Idle)
		}
		if listW >= 85 {
			lines := strings.Split(strings.TrimSpace(clean(a.Output)), "\n")
			text += "  " + strings.TrimSpace(lines[len(lines)-1])
		}
		r := itemRow(i == m.Selected, text)
		r.Text = clip(r.Text, listW-1)
		rows = append(rows, r)
	}
	a := m.Data.View.Agents[m.Selected]
	modelName := a.Model
	if modelName == "" {
		modelName = "CLI default"
	}
	preview := []string{"TERMINAL / " + a.Name, "CLI: " + a.Harness + "   configured model: " + modelName}
	if !a.Changed.IsZero() {
		preview = append(preview, "Space changed "+time.Since(a.Changed).Round(time.Second).String()+" ago")
	}
	if a.Error != "" {
		preview = append(preview, "Error: "+a.Error)
	}
	output := strings.TrimSpace(clean(a.Output))
	if output == "" {
		output = "No captured output. Start the company with s."
	}
	if wide {
		lines := strings.Split(output, "\n")
		n := max(1, h-len(preview))
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		preview = append(preview, lines...)
		for i := 0; i < h; i++ {
			if i >= len(rows) {
				rows = append(rows, row{})
			}
			right := ""
			if i < len(preview) {
				right = preview[i]
			}
			rows[i].Text = pad(rows[i].Text, listW) + " | " + clip(right, w-listW-3)
		}
	} else if h-len(rows) >= 4 {
		rows = append(rows, row{" Terminal / " + a.Name, accent})
		lines := strings.Split(output, "\n")
		n := h - len(rows)
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, s := range lines {
			rows = append(rows, row{" " + s, dim})
		}
	}
	return rows
}
func (m *model) renderForm(w, h int) []row {
	f := m.Form
	rows := []row{{" " + f.Title, accent}, {" Ctrl-S saves the form; Esc cancels without saving.", dim}}
	// One field uses three rows, and the active field always stays in view.
	slots := max(1, (h-2)/3)
	start := max(0, f.Selected-slots+1)
	for i := start; i < min(len(f.Fields), start+slots); i++ {
		v := f.Fields[i]
		label := v.Label
		if len(v.Choices) > 0 {
			label += "  [Left/Right choices]"
		}
		rows = append(rows, itemRow(i == f.Selected, label))
		value := strings.ReplaceAll(v.Value, "\n", " / ")
		if i == f.Selected && f.Editing {
			rs := []rune(v.Value)
			cursor := max(0, min(f.Cursor, len(rs)))
			value = string(rs[:cursor]) + "|" + string(rs[cursor:])
			value = strings.ReplaceAll(value, "\n", " / ")
			if width(value) > w-5 {
				r := []rune(value)
				from := max(0, cursor-(w-8)/2)
				value = "..." + string(r[from:])
			}
		}
		if value == "" {
			value = "(empty)"
		}
		rows = append(rows, row{"     " + value, ""}, row{})
	}
	return rows
}
func fitRows(rows []row, w, h int) []row {
	for i := range rows {
		rows[i].Text = clip(rows[i].Text, max(0, w))
	}
	if len(rows) > h {
		return rows[:h]
	}
	for len(rows) < h {
		rows = append(rows, row{})
	}
	return rows
}
func draw(rows []row, color bool) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, r := range rows {
		if color {
			b.WriteString(r.Style)
		}
		b.WriteString(r.Text)
		b.WriteString("\x1b[0m\x1b[K")
		if i < len(rows)-1 {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}
