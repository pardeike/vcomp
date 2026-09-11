package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Field kinds. A field with no kind is free text.
const (
	kindText     = ""
	kindStatic   = "static"   // shown, never selected
	kindChoice   = "choice"   // one of Options; Left/Right cycles, Enter picks, typing filters
	kindMulti    = "multi"    // comma-separated roster; number keys set profession counts
	kindFile     = "file"     // a path; Enter browses, typing edits
	kindDir      = "dir"      // a directory path; Enter browses
	kindNumber   = "number"   // Left/Right step, typing edits; empty inherits
	kindDuration = "duration" // Left/Right step through Options, Enter picks
)

type option struct {
	Value, Title string
	Dir          bool
}
type field struct {
	Label, Value, Kind string
	Empty              string // what an empty value means, shown dimmed
	Options            []option
	Base               string // directory that relative paths in file fields refer to
}
type picker struct {
	Items  []option
	Counts map[string]int
	Order  []string // the multi field's existing entries, whose order is kept
	Filter string
	Cursor int
	Dir    string
}
type form struct {
	Kind, Title      string
	Fields           []field
	Selected, Cursor int
	Editing          bool
	Picker           *picker
	Changed          func(f *form, i int) // called after a field's value changes
}

func newForm(kind, title string, fields []field, changed func(*form, int)) *form {
	f := &form{Kind: kind, Title: title, Fields: fields, Changed: changed}
	f.Selected = f.next(-1, 1)
	return f
}
func (f *form) values() []string {
	values := []string{}
	for _, v := range f.Fields {
		values = append(values, v.Value)
	}
	return values
}

// next moves from i by step (wrapping) to the nearest selectable field.
func (f *form) next(i, step int) int {
	for n := 0; n < len(f.Fields); n++ {
		i = (i + step + len(f.Fields)) % len(f.Fields)
		if f.Fields[i].Kind != kindStatic {
			return i
		}
	}
	return 0
}
func (f *form) changed(i int) {
	if f.Changed != nil {
		f.Changed(f, i)
	}
}
func (v field) option(value string) (option, bool) {
	for _, o := range v.Options {
		if o.Value == value {
			return o, true
		}
	}
	return option{}, false
}
func (v field) index() int {
	for i, o := range v.Options {
		if o.Value == v.Value {
			return i
		}
	}
	return -1
}

// display renders a field's value for the form list.
func (v field) display() (string, bool) {
	if v.Value == "" {
		if v.Empty != "" {
			return v.Empty, true
		}
		if o, ok := v.option(""); ok && o.Title != "" {
			return o.Title, true
		}
		return "empty", true
	}
	if o, ok := v.option(v.Value); ok && o.Title != "" && v.Kind == kindChoice {
		return v.Value + "  " + o.Title, false
	}
	return strings.ReplaceAll(v.Value, "\n", " / "), false
}

// hint names what the keys do for the selected field.
func (v field) hint() string {
	switch v.Kind {
	case kindChoice:
		return "Enter choose · ←/→ cycle · type to search"
	case kindMulti:
		return "Enter choose · type to edit the list"
	case kindFile:
		return "Enter browse · type a path"
	case kindDir:
		return "Enter browse · type a path"
	case kindNumber:
		return "←/→ adjust · type a number"
	case kindDuration:
		return "Enter choose · ←/→ adjust · type a duration"
	}
	return "Enter edit · type to replace"
}

func (m *model) formKey(k key) *action {
	f := m.Form
	if f.Picker != nil {
		m.pickerKey(k)
		return nil
	}
	if k.Name == "esc" {
		if f.Editing {
			f.Editing = false
		} else {
			m.Form = nil
		}
		return nil
	}
	if k.Name == "save" {
		return &action{Kind: f.Kind, Values: f.values()}
	}
	if k.Name == "tab" || k.Name == "backtab" || (!f.Editing && (k.Name == "up" || k.Name == "down")) {
		step := 1
		if k.Name == "backtab" || k.Name == "up" {
			step = -1
		}
		f.Selected = f.next(f.Selected, step)
		f.Editing = false
		return nil
	}
	v := &f.Fields[f.Selected]
	if !f.Editing {
		if k.Name == "left" || k.Name == "right" {
			step := 1
			if k.Name == "left" {
				step = -1
			}
			if f.step(step) {
				f.changed(f.Selected)
			}
			return nil
		}
		if k.Name == "enter" {
			switch v.Kind {
			case kindChoice, kindMulti, kindFile, kindDir, kindDuration:
				m.openPicker("")
			default:
				f.Editing = true
				f.Cursor = utf8.RuneCountInString(v.Value)
			}
			return nil
		}
		if k.Text == "" && k.Name != "paste" && k.Name != "clear" && k.Name != "backspace" {
			return nil
		}
		if v.Kind == kindChoice {
			m.openPicker(k.Text)
			return nil
		}
		f.Editing = true
		f.Cursor = utf8.RuneCountInString(v.Value)
	}
	rs := []rune(v.Value)
	f.Cursor = max(0, min(f.Cursor, len(rs)))
	before := v.Value
	switch k.Name {
	case "enter":
		f.Editing = false
		return nil
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
		if v.Kind == kindNumber {
			txt = digits(txt)
		}
		next := append([]rune{}, rs[:f.Cursor]...)
		next = append(next, txt...)
		next = append(next, rs[f.Cursor:]...)
		v.Value = string(next)
		f.Cursor += len(txt)
	}
	if v.Value != before {
		f.changed(f.Selected)
	}
	return nil
}
func digits(rs []rune) []rune {
	out := rs[:0:0]
	for _, r := range rs {
		if r >= '0' && r <= '9' {
			out = append(out, r)
		}
	}
	return out
}

// step moves a choice, number, or duration field by one notch and reports
// whether the value changed.
func (f *form) step(step int) bool {
	v := &f.Fields[f.Selected]
	before := v.Value
	switch v.Kind {
	case kindChoice:
		if len(v.Options) == 0 {
			return false
		}
		i := v.index()
		if i < 0 && step > 0 {
			i = -1
		}
		v.Value = v.Options[(i+step+len(v.Options))%len(v.Options)].Value
	case kindNumber:
		n := 0
		for _, r := range v.Value {
			if r >= '0' && r <= '9' {
				n = n*10 + int(r-'0')
			}
		}
		n = max(0, n+step)
		v.Value = ""
		if n > 0 {
			v.Value = itoa(n)
		}
	case kindDuration:
		cur, err := time.ParseDuration(v.Value)
		if err != nil {
			cur = 0
		}
		next := ""
		if step > 0 {
			for _, o := range v.Options {
				if d, err := time.ParseDuration(o.Value); err == nil && d > cur {
					next = o.Value
					break
				}
			}
		} else {
			for _, o := range v.Options {
				if d, err := time.ParseDuration(o.Value); err == nil && d < cur {
					next = o.Value
				}
			}
		}
		if next != "" {
			v.Value = next
		}
	default:
		return false
	}
	return v.Value != before
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// openPicker starts a chooser for the selected field: a filtered list for
// choices, a checklist for multi fields, or a directory browser for paths.
func (m *model) openPicker(filter string) {
	f := m.Form
	v := f.Fields[f.Selected]
	p := &picker{Filter: filter}
	switch v.Kind {
	case kindFile, kindDir:
		p.Dir = browseStart(v)
		p.Items = browse(p.Dir, v.Kind == kindDir)
		for i, o := range p.Items {
			if o.Value == absolute(v.Value, v.Base) {
				p.Cursor = i
			}
		}
	case kindMulti:
		p.Counts = map[string]int{}
		known := map[string]bool{}
		for _, o := range v.Options {
			known[o.Value] = true
		}
		p.Items = append(p.Items, v.Options...)
		for _, s := range strings.Split(v.Value, ",") {
			if s = strings.TrimSpace(s); s != "" {
				base := p.group(s)
				p.Counts[base]++
				p.Order = append(p.Order, s)
				if !known[base] {
					known[base] = true
					p.Items = append(p.Items, option{Value: base})
				}
			}
		}
	default:
		p.Items = v.Options
		if i := v.index(); i >= 0 {
			p.Cursor = i
		}
	}
	f.Picker = p
	f.Editing = false
}
func absolute(path, base string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) && base != "" {
		path = filepath.Join(base, path)
	}
	return filepath.Clean(path)
}
func browseStart(v field) string {
	path := absolute(v.Value, v.Base)
	if path != "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() && v.Kind == kindDir {
			return path
		}
		if _, err := os.Stat(filepath.Dir(path)); err == nil {
			return filepath.Dir(path)
		}
	}
	if v.Base != "" {
		if _, err := os.Stat(v.Base); err == nil {
			return v.Base
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return string(filepath.Separator)
}

// browse lists a directory: this directory (for directory fields), the parent,
// subdirectories, then files. Dot-entries are skipped; a path can still be typed.
func browse(dir string, dirsOnly bool) []option {
	items := []option{}
	if dirsOnly {
		items = append(items, option{Value: dir, Title: "choose this directory"})
	}
	if parent := filepath.Dir(dir); parent != dir {
		items = append(items, option{Value: parent, Title: "..", Dir: true})
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return append(items, option{Title: err.Error()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	for _, e := range entries {
		if !e.IsDir() && dirsOnly || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		items = append(items, option{Value: filepath.Join(dir, e.Name()), Title: e.Name(), Dir: e.IsDir()})
	}
	return items
}

// visible applies the filter. A typed value that is not itself a choice is
// offered after the matches, so a chooser never blocks a value the config takes.
func (p *picker) visible(v field) []option {
	filter := strings.ToLower(strings.TrimSpace(p.Filter))
	out := []option{}
	exact := false
	for _, o := range p.Items {
		if o.Value == p.Filter {
			exact = true
		}
		if filter == "" || strings.Contains(strings.ToLower(o.Value+" "+o.Title), filter) {
			out = append(out, o)
		}
	}
	if p.Filter != "" && !exact && (v.Kind == kindChoice || v.Kind == kindDuration) {
		out = append(out, option{Value: p.Filter, Title: "use this value"})
	}
	return out
}
func (m *model) pickerKey(k key) {
	f := m.Form
	p := f.Picker
	v := &f.Fields[f.Selected]
	items := p.visible(*v)
	p.Cursor = max(0, min(p.Cursor, len(items)-1))
	switch k.Name {
	case "esc":
		f.Picker = nil
	case "up":
		p.Cursor = max(0, p.Cursor-1)
	case "down":
		p.Cursor = min(len(items)-1, p.Cursor+1)
	case "pgup":
		p.Cursor = max(0, p.Cursor-max(1, m.H-10))
	case "pgdown":
		p.Cursor = min(len(items)-1, p.Cursor+max(1, m.H-10))
	case "home":
		p.Cursor = 0
	case "end":
		p.Cursor = max(0, len(items)-1)
	case "clear":
		p.Filter = ""
		p.Cursor = 0
	case "backspace":
		if p.Filter != "" {
			rs := []rune(p.Filter)
			p.Filter = string(rs[:len(rs)-1])
			p.Cursor = 0
		} else if p.Dir != "" {
			m.browseTo(filepath.Dir(p.Dir))
		}
	case "enter":
		if len(items) == 0 {
			return
		}
		item := items[p.Cursor]
		if p.Counts != nil {
			if p.Counts[item.Value] > 0 {
				p.Counts[item.Value] = 0
			} else {
				p.Counts[item.Value] = 1
			}
			return
		}
		switch {
		case p.Dir != "" && item.Dir:
			m.browseTo(item.Value)
			return
		case p.Dir != "":
			if item.Value == "" {
				return
			}
			v.Value = relative(item.Value, v.Base)
		default:
			v.Value = item.Value
		}
		f.Picker = nil
		f.changed(f.Selected)
	case "tab":
		if p.Counts != nil {
			v.Value = p.joined()
			f.Picker = nil
			f.changed(f.Selected)
		}
	default:
		if p.Counts != nil && len(items) > 0 {
			name := items[p.Cursor].Value
			if k.Text == " " {
				if p.Counts[name] > 0 {
					p.Counts[name] = 0
				} else {
					p.Counts[name] = 1
				}
				return
			}
			if len(k.Text) == 1 && k.Text[0] >= '0' && k.Text[0] <= '9' {
				n := int(k.Text[0] - '0')
				if name == "ceo" {
					n = min(n, 1)
				}
				p.Counts[name] = n
				return
			}
		}
		if k.Text != "" || k.Name == "paste" {
			p.Filter += strings.ReplaceAll(k.Text, "\n", "")
			p.Cursor = 0
		}
	}
}

// group folds numbered employees into their profession's chooser row.
func (p *picker) group(name string) string {
	for _, o := range p.Items {
		if o.Value == name {
			return name
		}
		if suffix, ok := strings.CutPrefix(name, o.Value+"-"); ok {
			if n, err := strconv.Atoi(suffix); err == nil && n > 0 {
				return o.Value
			}
		}
	}
	return name
}

func (p *picker) joined() string {
	original := map[string]int{}
	for _, name := range p.Order {
		original[p.group(name)]++
	}
	out := []string{}
	seen := map[string]bool{}
	add := func(base string) {
		if seen[base] {
			return
		}
		seen[base] = true
		n := p.Counts[base]
		if n == 1 {
			out = append(out, base)
		} else {
			for i := 1; i <= n; i++ {
				out = append(out, base+"-"+strconv.Itoa(i))
			}
		}
	}
	for _, name := range p.Order {
		base := p.group(name)
		if p.Counts[base] == original[base] {
			out = append(out, name) // An unchanged count preserves names and ordering.
			seen[base] = true
		} else {
			add(base)
		}
	}
	for _, o := range p.Items {
		add(o.Value)
	}
	return strings.Join(out, ", ")
}
func (m *model) browseTo(dir string) {
	p := m.Form.Picker
	from := p.Dir
	p.Dir = dir
	p.Items = browse(dir, m.Form.Fields[m.Form.Selected].Kind == kindDir)
	p.Filter = ""
	p.Cursor = 0
	for i, o := range p.Items {
		if o.Value == from && o.Dir {
			p.Cursor = i
		}
	}
}
func relative(path, base string) string {
	if base == "" {
		return path
	}
	if rel, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}
