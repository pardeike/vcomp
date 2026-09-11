package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A row is one terminal line. Text is the plain content; Spans, when present,
// carry the same text in styled pieces. Style is the base for the whole row.
type span struct{ Text, Style string }
type row struct {
	Text, Style string
	Spans       []span
}

const (
	accent   = "\x1b[1;36m"
	selected = "\x1b[30;46m"
	dim      = "\x1b[2m"
	warning  = "\x1b[33m"
	bold     = "\x1b[1m"
	good     = "\x1b[32m"
	bad      = "\x1b[31m"
	inverse  = "\x1b[7m"
)

func styled(spans ...span) row {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return row{Text: b.String(), Spans: spans}
}
func rule(w int) row { return row{Text: strings.Repeat("─", max(0, w)), Style: dim} }

// hints joins key/label pairs, dropping trailing pairs that do not fit, with a
// right-aligned trailer such as the terminal size.
func hints(w int, trailer string, pairs ...string) row {
	spans := []span{}
	used := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		piece := []span{{" " + pairs[i], bold}, {" " + pairs[i+1] + " ", dim}}
		n := width(piece[0].Text) + width(piece[1].Text)
		if used+n+width(trailer)+1 > w {
			break
		}
		spans = append(spans, piece...)
		used += n
	}
	if trailer != "" && used+width(trailer)+1 <= w {
		spans = append(spans, span{strings.Repeat(" ", w-used-width(trailer)-1) + trailer + " ", dim})
	}
	return styled(spans...)
}

func (m *model) render(w, h int) []row {
	m.W, m.H = w, h
	m.CursorX, m.CursorY = -1, -1
	if w < 40 || h < 12 {
		return fitRows([]row{{Text: "vcomp", Style: accent}, {Text: "Terminal too small", Style: warning}, {Text: "Use at least 40 columns x 12 rows.", Style: ""}, {Text: "q or Ctrl-C to exit", Style: dim}}, w, h)
	}
	rows := []row{m.header(w), m.tabs(w), rule(w)}
	contentH := h - 6
	body := []row{}
	switch {
	case m.Confirm != nil:
		body = m.renderConfirm(w)
	case m.Form != nil && m.Form.Picker != nil:
		body = m.renderPicker(w, contentH, len(rows))
	case m.Form != nil:
		body = m.renderForm(w, contentH, len(rows))
	case m.Detail != "" || m.count() == 0 && m.Screen != 0 && m.Screen != 1 && m.Screen != 6:
		body = m.renderDocument(w, contentH)
	case m.Screen == 0:
		body = m.dashboard(w, contentH)
	case m.Screen == 1:
		columns := []string{"Run", "State", "Created"}
		if w >= 95 {
			columns = append(columns, "Attempt", "Impressions excerpt")
		}
		body = m.list(w, contentH, "Public user tests", "S sort: "+m.Data.View.Config.UISort["public_tests_sort"], columns, func(i int) []string {
			r := m.Data.View.Runs[i]
			created := "—"
			if !r.Created.IsZero() {
				created = r.Created.Local().Format("Jan 02 15:04")
				if r.CreatedApprox {
					created = "~" + created
				}
			}
			cells := []string{clip(r.Name, max(12, w/4)), r.State, created}
			if w >= 95 {
				cells = append(cells, fmt.Sprint(r.Attempts), m.Data.RunExcerpts[r.Name])
			}
			return cells
		}, "No public tests yet. Press n to send in a user.")
	case m.Screen == 6:
		body = m.list(w, contentH, "Professions", "S sort: "+m.Data.View.Config.UISort["catalogue_sort"], []string{"Profession", "Title", "Sector", "State"}, func(i int) []string {
			p := m.catalogue()[i]
			state := "active"
			if p.Deleted {
				state = "deleted"
			}
			return []string{p.Name, p.Title, p.Sector, state}
		}, "No professions found.")
	}
	for len(body) < contentH {
		body = append(body, row{})
	}
	if len(body) > contentH {
		body = body[:contentH]
	}
	rows = append(rows, body...)
	message := m.Message
	style := warning
	if m.Busy {
		message, style = "Working...", dim
	}
	if m.Data.View.Error != "" && message == "" {
		message = m.Data.View.Error
	}
	rows = append(rows, rule(w), row{Text: " " + message, Style: style}, m.footer(w, h))
	return fitRows(rows, w, h)
}
func (m *model) header(w int) row {
	left := []span{{" " + m.rootLabel(), bold}}
	status := m.status()
	style := dim
	switch status {
	case "RUNNING / UPDATING":
		style = good
	case "UNSUPERVISED AGENTS":
		style = warning
	case "FINISHED":
		style = accent
	}
	right := []span{{status, style}}
	if !m.Data.View.ObservedAt.IsZero() && w >= 65 {
		right = append(right, span{" · tick " + time.Since(m.Data.View.ObservedAt).Round(time.Second).String() + " ago", dim})
	} else if w >= 65 {
		right = append(right, span{" · no engine observation", dim})
	}
	l, r := 0, 0
	for _, s := range left {
		l += width(s.Text)
	}
	for _, s := range right {
		r += width(s.Text)
	}
	spans := append([]span{}, left...)
	if l+r+2 <= w {
		spans = append(spans, span{strings.Repeat(" ", w-l-r-1), ""})
		spans = append(spans, right...)
	}
	return styled(spans...)
}
func (m *model) tabs(w int) row {
	spans := []span{{" ", ""}}
	for i, s := range screens {
		if i == m.Screen {
			spans = append(spans, span{fmt.Sprintf(" %d %s ", i+1, s), selected}, span{" ", ""})
		} else {
			spans = append(spans, span{fmt.Sprintf(" %d", i+1), dim}, span{" " + s + "  ", ""})
		}
	}
	full := styled(spans...)
	if width(full.Text) <= w {
		return full
	}
	hint := "  Tab next screen"
	if m.Detail != "" {
		hint = "  1–7 screens"
	}
	if m.Form != nil || m.Confirm != nil {
		hint = ""
	}
	return styled(span{fmt.Sprintf(" %d/%d ", m.Screen+1, len(screens)), dim}, span{screens[m.Screen], accent}, span{hint, dim})
}
func (m *model) footer(w, h int) row {
	size := fmt.Sprintf("%dx%d", w, h)
	switch {
	case m.Confirm != nil:
		return hints(w, size, "y", "confirm", "n / Esc", "cancel")
	case m.Form != nil && m.Form.Picker != nil:
		p := m.Form.Picker
		if p.Counts != nil {
			return hints(w, size, "0–9", "count", "Enter", "toggle", "Tab", "done", "↑↓", "move", "Esc", "cancel")
		}
		if p.Dir != "" {
			return hints(w, size, "Enter", "open / choose", "Backspace", "parent", "↑↓", "move", "Esc", "back", "type", "filter")
		}
		return hints(w, size, "Enter", "choose", "↑↓", "move", "Esc", "back", "type", "filter")
	case m.Form != nil:
		if m.Form.Editing {
			return hints(w, size, "Enter", "done", "Ctrl-U", "clear", "Ctrl-S", "save", "Esc", "stop editing")
		}
		return hints(w, size, "Tab / ↑↓", "field", "Ctrl-S", "save", "Esc", "cancel")
	case m.Detail == "profession-draft":
		return hints(w, size, "e", "edit draft", "s", "save", "↑↓", "scroll", "Esc", "back")
	case m.Detail == "profession":
		return hints(w, size, "e", "edit", "g", "generate", "h", "hire", "d", "delete", "u", "restore", "Esc", "back")
	case m.Detail == "agent" && m.Sub == 0:
		return hints(w, size, "m", "message", "←→", "request", "Tab", "next tab", "↑↓", "scroll", "Esc", "back", "[ / ]", "request")
	case m.Detail == "agent" && m.Sub == 2:
		state := "paused"
		if m.Follow {
			state = "following"
		}
		return hints(w, size, "v", "verbosity", "f", "follow ("+state+")", "↑↓", "scroll", "Tab", "next tab", "Esc", "back", "i", "direct steer", "a", "intervene", "m", "message")
	case m.Detail == "agent":
		return hints(w, size, "i", "direct steer", "m", "message", "Tab", "next tab", "↑↓", "scroll", "Esc", "back", "a", "intervene", "t", "steer", "p", "settings", "b", "replace", "e", "edit")
	case m.Detail != "":
		return hints(w, size, "↑↓", "scroll", "Esc", "back", "q", "leave")
	}
	switch m.Screen {
	case 0:
		if len(m.Data.View.Agents) == 0 {
			return hints(w, size, "c", "set up", "o", "open", "?", "help", "q", "leave")
		}
		return hints(w, size, "Enter", "open role", "S", "sort", "i", "direct steer", "B", "broadcast", "m", "message", "s", "start", "x", "stop", "h", "hire", "t", "steer", "a", "intervene", "?", "help", "q", "leave")
	case 1:
		return hints(w, size, "Enter", "open test", "S", "sort", "n", "new test", "?", "help", "q", "leave")
	case 2:
		return hints(w, size, "Enter", "diff", "↑↓", "scroll", "?", "help", "q", "leave")
	case 3:
		return hints(w, size, "Enter", "edit settings", "g", "role generator", "e", "edit file", "?", "help", "q", "leave")
	case 4:
		return hints(w, size, "e", "edit goal file", "↑↓", "scroll", "?", "help", "q", "leave")
	case 5:
		return hints(w, size, "↑↓", "scroll", "f", "follow", "?", "help", "q", "leave")
	}
	return hints(w, size, "Enter", "inspect", "S", "sort", "n", "new", "h", "hire", "e", "edit", "d", "delete", "z", "deleted", "p", "generator", "?", "help", "q", "leave")
}
func (m *model) renderConfirm(w int) []row {
	body := []row{{Text: " Confirm action", Style: warning}, {}}
	for _, s := range wrap(m.Confirmation, w-2) {
		body = append(body, row{Text: " " + s})
	}
	return append(body, row{}, styled(span{" y", bold}, span{" confirm    ", dim}, span{"n / Esc", bold}, span{" cancel", dim}))
}
func (m *model) renderDocument(w, h int) []row {
	lines := wrap(m.document(), w-2)
	if len(lines) == 0 {
		lines = []string{"Nothing here yet."}
	}
	body := []row{}
	if m.Detail == "agent" {
		spans := []span{{" " + strings.ToUpper(m.agent()) + " ", inverse + bold}, {"  ", ""}}
		for i, s := range []string{"Inbox", "Notes", "Terminal", "Goals", "Role"} {
			if i == m.Sub {
				spans = append(spans, span{" " + s + " ", selected}, span{" ", ""})
			} else {
				spans = append(spans, span{" " + s + "  ", dim})
			}
		}
		tabs := styled(spans...)
		if width(tabs.Text) > w {
			tabs = styled(span{" " + strings.ToUpper(m.agent()) + " ", inverse + bold}, span{fmt.Sprintf(" · %d/5 ", m.Sub+1), dim}, span{[]string{"Inbox", "Notes", "Terminal", "Goals", "Role"}[m.Sub], selected})
		}
		body = append(body, tabs)
		if m.Sub == 0 {
			requests := m.Data.Inboxes[m.agent()]
			if len(requests) > 0 {
				index := m.inboxIndex()
				body = append(body, row{Text: fmt.Sprintf(" Request %d of %d · %s", index+1, len(requests), requests[index].Name), Style: accent})
			}
		}
		body = append(body, rule(w))
	} else {
		body = append(body, row{Text: " " + m.title(), Style: accent})
	}
	if m.Detail == "agent" && m.Sub == 2 {
		if status, records, ok := strings.Cut(m.document(), "\n── "); ok {
			title, content, _ := strings.Cut(records, "\n")
			metadata := wrap(strings.TrimSpace(status), w-2)
			limit := max(0, h-len(body)-4)
			for _, line := range metadata[:min(len(metadata), limit)] {
				body = append(body, row{Text: " " + line, Style: dim})
			}
			label := clip("── "+title, w-2)
			body = append(body, row{Text: " " + label + strings.Repeat("─", max(0, w-width(label)-2)), Style: accent})
			lines = wrap(content, w-2)
		}
	}
	size := h - len(body)
	if m.Follow && (m.Screen == 5 && m.Detail == "" || m.Detail == "agent" && m.Sub == 2) {
		m.Scroll = max(0, len(lines)-size)
	}
	m.Scroll = max(0, min(m.Scroll, max(0, len(lines)-size)))
	if len(lines) > size {
		position := fmt.Sprintf("%d-%d of %d", m.Scroll+1, min(len(lines), m.Scroll+size), len(lines))
		if m.Follow && (m.Screen == 5 && m.Detail == "" || m.Detail == "agent" && m.Sub == 2) {
			position = "following · " + position
		}
		title := &body[0]
		if width(title.Text)+width(position)+2 <= w {
			title.Spans = append(title.Spans, span{strings.Repeat(" ", w-width(title.Text)-width(position)-1) + position, dim})
			if len(title.Spans) == 1 {
				title.Spans = append([]span{{title.Text, title.Style}}, title.Spans...)
			}
			title.Text += title.Spans[len(title.Spans)-1].Text
		}
	}
	for _, s := range lines[m.Scroll:min(len(lines), m.Scroll+size)] {
		if strings.HasPrefix(s, "── ") && strings.HasSuffix(s, " ──") {
			body = append(body, row{Text: " " + s + strings.Repeat("─", max(0, w-width(s)-2)), Style: accent})
		} else {
			body = append(body, row{Text: " " + s})
		}
	}
	return body
}

// list draws a titled, column-aligned selectable table.
func (m *model) list(w, h int, title, keys string, columns []string, cell func(int) []string, empty string) []row {
	body := []row{styled(span{" " + title, accent}, span{"    " + keys, dim})}
	if m.count() == 0 {
		return append(body, row{Text: " " + empty, Style: dim})
	}
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = width(c)
	}
	for i := 0; i < m.count(); i++ {
		for j, s := range cell(i) {
			widths[j] = max(widths[j], width(s))
		}
	}
	line := func(cells []string) string {
		parts := []string{}
		for j, s := range cells {
			if j == len(cells)-1 {
				parts = append(parts, s)
			} else {
				parts = append(parts, pad(s, widths[j]))
			}
		}
		return strings.Join(parts, "  ")
	}
	body = append(body, row{Text: "   " + line(columns), Style: dim})
	size := h - len(body)
	start := max(0, m.Selected-size+1)
	for i := start; i < min(m.count(), start+size); i++ {
		body = append(body, itemRow(i == m.Selected, line(cell(i))))
	}
	return body
}
func itemRow(active bool, s string) row {
	if active {
		return row{Text: " > " + s, Style: selected}
	}
	return row{Text: "   " + s}
}
func stateStyle(state string) string {
	switch state {
	case "live":
		return good
	case "quiet", "starting", "pending":
		return warning
	case "error", "broken", "abandoned":
		return bad
	case "done":
		return accent
	}
	return dim
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
		if r.State == "done" {
			done++
		}
	}
	if len(m.Data.View.Agents) == 0 {
		return []row{{Text: " No company in this directory yet.", Style: accent}, {}, styled(span{" c", bold}, span{" set up a company", dim}), styled(span{" o", bold}, span{" open another directory", dim}), {}, {Text: " Setup only writes files when you save.", Style: dim}}
	}
	rows := []row{styled(span{fmt.Sprintf(" %d agents", len(m.Data.View.Agents)), accent}, span{fmt.Sprintf(" · %d live · %d inbox · tests %d/%d", live, inbox, done, len(m.Data.View.Runs)), dim}), {Text: " Product: " + m.Data.ProductSummary, Style: dim}}
	if h >= 24 {
		for i := 1; i < min(3, len(m.Data.RecentCommits)); i++ {
			rows = append(rows, row{Text: "          " + m.Data.RecentCommits[i], Style: dim})
		}
	}
	wide := w >= 120
	listW := w
	if wide {
		listW = w * 55 / 100
	}
	listH := h - len(rows) - 1
	if !wide && h >= 17 {
		listH = max(5, h*2/3-len(rows)-1)
	}
	nameW := 8
	for _, a := range m.Data.View.Agents {
		nameW = min(24, max(nameW, width(a.Name)))
	}
	// Timing takes priority over CLI/idle. Add each column independently so
	// smaller terminals use their spare cells instead of hiding the whole set.
	baseWidth := 3 + nameW + 2 + 9 + 2 + 5
	fullWidth := baseWidth + 23 + 16
	if wide && w >= fullWidth+1+3+40 {
		listW = max(listW, fullWidth+1)
	}
	showTurns := listW >= baseWidth+6+1
	showStarted := listW >= baseWidth+15+1
	showAverage := listW >= baseWidth+23+1
	showCLI := listW >= fullWidth+1
	showLastLine := listW >= fullWidth+18+1
	header := "   " + pad("Agent", nameW) + "  " + pad("State", 9) + "  Inbox"
	if showCLI {
		header += "  " + pad("CLI", 8) + "  Idle"
	}
	if showTurns {
		header += fmt.Sprintf(" %5s", "Turns")
	}
	if showStarted {
		header += fmt.Sprintf(" %-8s", "Started")
	}
	if showAverage {
		header += fmt.Sprintf(" %7s", "Avg")
	}
	if showLastLine {
		header += "  Last line"
	}
	rows = append(rows, row{Text: header, Style: dim})
	start := max(0, m.Selected-listH+1)
	for i := start; i < min(m.count(), start+listH); i++ {
		a := m.Data.View.Agents[i]
		cells := []span{{pad(a.Name, nameW) + "  ", ""}, {pad(a.State, 9), stateStyle(a.State)}, {fmt.Sprintf("  %5d", a.Inbox), ""}}
		if showCLI {
			cells = append(cells, span{fmt.Sprintf("  %s  %4d", pad(a.Harness, 8), a.Idle), ""})
		}
		if showTurns {
			count, started, average := "—", "—", "—"
			if a.Turns.Known {
				count = fmt.Sprint(a.Turns.Completed)
				if !a.Turns.Started.IsZero() {
					started = a.Turns.Started.Local().Format("15:04:05")
				}
				if a.Turns.Completed > 0 {
					average = a.Turns.Average.Round(time.Second).String()
				}
			}
			cells = append(cells, span{fmt.Sprintf(" %5s", clip(count, 5)), dim})
			if showStarted {
				cells = append(cells, span{fmt.Sprintf(" %-8s", started), dim})
			}
			if showAverage {
				cells = append(cells, span{fmt.Sprintf(" %7s", clip(average, 7)), dim})
			}
		}
		if showLastLine {
			lines := strings.Split(strings.TrimSpace(clean(a.Output)), "\n")
			cells = append(cells, span{"  " + strings.TrimSpace(lines[len(lines)-1]), dim})
		}
		r := row{}
		if i == m.Selected {
			r = styled(append([]span{{" > ", selected}}, cells...)...)
			for j := range r.Spans {
				r.Spans[j].Style = selected
			}
			r.Style = selected
		} else {
			r = styled(append([]span{{"   ", ""}}, cells...)...)
		}
		rows = append(rows, clipRow(r, listW-1))
	}
	a := m.Data.View.Agents[m.Selected]
	modelName := a.Model
	if modelName == "" {
		modelName = "CLI default"
	}
	preview := []row{styled(span{a.Name, accent}, span{" · " + a.Harness + " · " + modelName, dim})}
	if !a.Changed.IsZero() {
		preview = append(preview, row{Text: "space changed " + time.Since(a.Changed).Round(time.Second).String() + " ago", Style: dim})
	}
	if a.Error != "" {
		preview = append(preview, row{Text: "Error: " + a.Error, Style: bad})
	}
	topics := m.Data.Inboxes[a.Name]
	if len(topics) > 0 {
		preview = append(preview, row{Text: "Inbox requests", Style: accent})
		for _, topic := range topics[:min(3, len(topics))] {
			preview = append(preview, row{Text: "  " + topic.Name})
		}
		if len(topics) > 3 {
			preview = append(preview, row{Text: fmt.Sprintf("  +%d more · Enter → inbox", len(topics)-3), Style: dim})
		}
	}
	output := strings.TrimSpace(clean(a.Output))
	if output == "" {
		output = "No captured output. Start the company with s."
	}
	if wide {
		rightW := w - listW - 3
		preview = append(preview, rule(rightW))
		lines := strings.Split(output, "\n")
		n := max(1, h-len(preview))
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, s := range lines {
			preview = append(preview, row{Text: s})
		}
		for i := 0; i < h; i++ {
			if i >= len(rows) {
				rows = append(rows, row{})
			}
			right := row{}
			if i < len(preview) {
				right = clipRow(preview[i], rightW)
			}
			left := clipRow(rows[i], listW)
			left.Text = pad(left.Text, listW)
			if left.Spans != nil {
				left.Spans = append(left.Spans, span{strings.Repeat(" ", listW-width(styled(left.Spans...).Text)), ""})
			} else {
				left.Spans = []span{{left.Text, left.Style}}
			}
			spans := append(left.Spans, span{" │ ", dim})
			if right.Spans != nil {
				spans = append(spans, right.Spans...)
			} else {
				spans = append(spans, span{right.Text, right.Style})
			}
			rows[i] = styled(spans...)
		}
	} else if h-len(rows) >= 4 {
		rows = append(rows, rule(w))
		for _, r := range preview[:min(len(preview), h-len(rows)-1)] {
			rows = append(rows, r)
		}
		lines := strings.Split(output, "\n")
		n := h - len(rows)
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		for _, s := range lines {
			rows = append(rows, row{Text: " " + s, Style: dim})
		}
	}
	return rows
}

// renderForm lays fields out as one aligned row each: label, then value. The
// selected field is highlighted, and a hint below the list explains its keys.
func (m *model) renderForm(w, h, top int) []row {
	f := m.Form
	rows := []row{{Text: " " + f.Title, Style: accent}, {}}
	labelW := 0
	for _, v := range f.Fields {
		labelW = max(labelW, width(v.Label))
	}
	labelW = min(labelW, max(10, w/3))
	valueW := w - labelW - 7
	slots := max(1, h-4)
	start := max(0, f.Selected-slots+1)
	for i := start; i < min(len(f.Fields), start+slots); i++ {
		v := f.Fields[i]
		value, placeholder := v.display()
		style := ""
		if placeholder {
			style = dim
		}
		if i == f.Selected && f.Editing {
			rs := []rune(v.Value)
			cursor := max(0, min(f.Cursor, len(rs)))
			from, prefix := 0, ""
			if width(string(rs[:cursor])) > valueW-1 {
				from, prefix = max(0, cursor-valueW/2), "…"
			}
			value = strings.ReplaceAll(prefix+string(rs[from:]), "\n", " / ")
			style = ""
			m.CursorX, m.CursorY = 5+labelW+width(prefix)+width(string(rs[from:cursor])), top+len(rows)
		}
		marker := "   "
		if i == f.Selected {
			marker = " > "
		}
		r := styled(span{marker + pad(v.Label, labelW) + "  ", ""}, span{clip(value, valueW), style})
		switch {
		case v.Kind == kindStatic:
			r.Style = dim
			r.Spans = nil
		case i == f.Selected && !f.Editing:
			r.Style = selected
			for j := range r.Spans {
				r.Spans[j].Style = selected
			}
		}
		rows = append(rows, r)
	}
	if f.Selected < len(f.Fields) {
		rows = append(rows, row{}, row{Text: "   " + f.Fields[f.Selected].hint(), Style: dim})
	}
	return rows
}

// renderPicker shows the chooser for the selected field: a filter line and
// the matching items, with the current one marked.
func (m *model) renderPicker(w, h, top int) []row {
	f := m.Form
	p := f.Picker
	v := f.Fields[f.Selected]
	title := []span{{" " + f.Title, dim}, {" · ", dim}, {v.Label, accent}}
	if p.Dir != "" {
		title = append(title, span{"  " + shorten(p.Dir, w-width(styled(title...).Text)-3), dim})
	}
	rows := []row{styled(title...), styled(span{"   filter ", dim}, span{p.Filter, ""})}
	m.CursorX, m.CursorY = 10+width(p.Filter), top+1
	items := p.visible(v)
	p.Cursor = max(0, min(p.Cursor, len(items)-1))
	size := h - len(rows)
	if len(items) == 0 {
		return append(rows, row{Text: "   No matches.", Style: dim})
	}
	valueW := 0
	for _, o := range items {
		valueW = max(valueW, width(pickerName(o)))
	}
	valueW = min(valueW, max(12, w/2))
	start := max(0, p.Cursor-size+1)
	for i := start; i < min(len(items), start+size); i++ {
		o := items[i]
		mark := " > "
		if i != p.Cursor {
			mark = "   "
		}
		if p.Counts != nil {
			if p.Counts[o.Value] > 0 {
				mark += fmt.Sprintf("[%d] ", p.Counts[o.Value])
			} else {
				mark += "[ ] "
			}
		}
		name := pickerName(o)
		r := styled(span{mark + pad(name, valueW), ""}, span{"  " + o.Title, dim})
		if o.Title == "" || o.Value == "" || p.Dir != "" {
			r = styled(span{mark + name, ""})
		}
		if i == p.Cursor {
			r.Style = selected
			for j := range r.Spans {
				r.Spans[j].Style = selected
			}
		}
		rows = append(rows, r)
	}
	return rows
}
func pickerName(o option) string {
	switch {
	case o.Value == "":
		return o.Title
	case o.Title == "..":
		return ".."
	case o.Title == "choose this directory":
		return "."
	case o.Dir:
		return o.Title + "/"
	}
	if o.Title != "" && (strings.Contains(o.Value, "/") || o.Title == "use this value") {
		if o.Title == "use this value" {
			return o.Value
		}
		return o.Title
	}
	return o.Value
}

// shorten abbreviates a path for a title: the home directory becomes ~, and a
// path still too long keeps its last segments.
func shorten(path string, n int) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home) {
		path = "~" + strings.TrimPrefix(path, home)
	}
	parts := strings.Split(path, string(filepath.Separator))
	for width(path) > n && len(parts) > 2 {
		parts = parts[1:]
		path = "…" + string(filepath.Separator) + strings.Join(parts[1:], string(filepath.Separator))
	}
	return path
}
func clipRow(r row, n int) row {
	r.Text = clip(r.Text, max(0, n))
	if r.Spans == nil {
		return r
	}
	left := max(0, n)
	spans := []span{}
	for _, s := range r.Spans {
		s.Text = clip(s.Text, left)
		left -= width(s.Text)
		spans = append(spans, s)
		if left <= 0 {
			break
		}
	}
	r.Spans = spans
	return r
}
func fitRows(rows []row, w, h int) []row {
	for i := range rows {
		rows[i] = clipRow(rows[i], w)
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
		if r.Spans == nil {
			if color {
				b.WriteString(r.Style)
			}
			b.WriteString(r.Text)
		} else {
			for _, s := range r.Spans {
				if color {
					b.WriteString(r.Style + s.Style)
				}
				b.WriteString(s.Text)
				if color {
					b.WriteString("\x1b[0m")
				}
			}
		}
		b.WriteString("\x1b[0m\x1b[K")
		if i < len(rows)-1 {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}
