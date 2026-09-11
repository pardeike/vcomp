package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vcomp/internal/config"
	"vcomp/internal/engine"
)

func fixture() model {
	m := model{Root: "/tmp/company", Data: data{View: engine.Observation{Exists: true, Config: config.Default()}, AgentDocs: map[string][]string{}}}
	for i := 0; i < 35; i++ {
		name := fmt.Sprintf("developer-%02d", i)
		m.Data.View.Agents = append(m.Data.View.Agents, engine.AgentView{Name: name, State: "quiet", Harness: "omp", Model: "local/model", Inbox: i, Output: "Read src/main.go\n正在测试\nwaiting for input"})
		m.Data.AgentDocs[name] = []string{"terminal", "inbox", "notes", "goals", "role"}
	}
	return m
}
func TestResponsiveLayoutKeepsSelectionAndBounds(t *testing.T) {
	for _, size := range [][2]int{{160, 48}, {120, 30}, {80, 24}, {60, 18}, {40, 12}, {20, 6}} {
		m := fixture()
		m.Selected = 34
		for screen := range screens {
			m.Screen = screen
			for _, detail := range []string{"", "agent", "help"} {
				m.Detail = detail
				rows := m.render(size[0], size[1])
				if len(rows) != size[1] {
					t.Fatalf("size %v: %d rows", size, len(rows))
				}
				for _, r := range rows {
					if width(r.Text) > size[0] || strings.Contains(r.Text, "\x1b") {
						t.Fatalf("overflow at %v: %q", size, r.Text)
					}
				}
			}
		}
		m.Screen = 0
		m.Detail = ""
		rows := m.render(size[0], size[1])
		if size[0] >= 40 && size[1] >= 12 && !strings.Contains(draw(rows, false), "developer-34") {
			t.Fatalf("selected agent is offscreen at %v", size)
		}
	}
}
func TestNavigationAndFormEditing(t *testing.T) {
	m := fixture()
	m.key(key{Name: "down"})
	m.key(key{Name: "enter"})
	if m.Detail != "agent" || m.agent() != "developer-01" {
		t.Fatal(m)
	}
	m.key(key{Name: "tab"})
	if m.document() != "inbox" {
		t.Fatal("detail tab did not change")
	}
	m.key(key{Name: "esc"})
	if m.Detail != "" {
		t.Fatal("back failed")
	}
	m.Form = &form{Kind: "hire", Fields: []field{{Label: "Name", Value: "ab"}, {Label: "Profession", Value: "developer"}}}
	m.key(key{Name: "enter"})
	m.key(key{Name: "left"})
	m.key(key{Text: "界"})
	m.key(key{Name: "backspace"})
	if m.Form.Fields[0].Value != "ab" {
		t.Fatal(m.Form.Fields[0].Value)
	}
	m.key(key{Name: "tab"})
	m.key(key{Name: "enter"})
	m.key(key{Name: "clear"})
	m.key(key{Name: "paste", Text: "tester"})
	a := m.key(key{Name: "save"})
	if a.Kind != "hire" || a.Values[1] != "tester" {
		t.Fatalf("bad submit: %+v", a)
	}
	m.key(key{Name: "esc"})
	m.key(key{Name: "esc"})
	if m.Form != nil {
		t.Fatal("cancel retained form")
	}
}
func TestDestructiveActionsRequireConfirmation(t *testing.T) {
	m := fixture()
	if a := m.key(key{Text: "x"}); a != nil {
		t.Fatal("stop ran without confirmation")
	}
	if m.Confirm == nil {
		t.Fatal("no confirmation")
	}
	m.key(key{Name: "esc"})
	if m.Confirm != nil {
		t.Fatal("cancel failed")
	}
	m.key(key{Text: "x"})
	a := m.key(key{Text: "y"})
	if a == nil || a.Kind != "stop" {
		t.Fatal("confirmation did not stop")
	}
	m.OwnEngine = true
	if a = m.key(key{Text: "q"}); a != nil || m.Confirm == nil {
		t.Fatal("leaving owned supervisor needs explanation")
	}
}
func TestTerminalInputAndControlSanitizing(t *testing.T) {
	for _, tt := range []struct{ s, name, text string }{{"\x1b[A", "up", ""}, {"\x1b", "esc", ""}, {"é", "", "é"}, {"\x1b[200~hello\nworld\x1b[201~", "paste", "hello\nworld"}} {
		k, n := decodeKey([]byte(tt.s), true)
		if n != len(tt.s) || k.Name != tt.name || k.Text != tt.text {
			t.Fatalf("%q: %+v %d", tt.s, k, n)
		}
	}
	input := "ok\x1b[2J\x1b]52;c;secret\x07text\r\n界é"
	got := clean(input)
	if got != "oktext\n界é" {
		t.Fatalf("controls survived: %q", got)
	}
	if width("界é") != 3 || clip("界é", 2) != "界" {
		t.Fatal("incorrect display width")
	}
}
func TestSettingsValidationDoesNotWriteOnFailure(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := filepath.Join(t.TempDir(), "new-company")
	f, err := settingsForm(root)
	if err != nil {
		t.Fatal(err)
	}
	values := []string{}
	for _, v := range f.Fields {
		values = append(values, v.Value)
	}
	values[0] = ""
	if err = saveSettings(root, values); err == nil {
		t.Fatal("empty goal accepted")
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid form created company")
	}
	values[0] = "Build something"
	values[3] = "missing-harness"
	if err = saveSettings(root, values); err == nil {
		t.Fatal("unknown harness accepted")
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid harness created company")
	}
	values[3] = "pi"
	values[2] = "ceo, developer"
	if err = saveSettings(root, values); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil || cfg.Harness != "pi" {
		t.Fatalf("saved config: %+v %v", cfg, err)
	}
	if _, err = os.Stat(filepath.Join(root, "spaces", "developer", "role.md")); err != nil {
		t.Fatal(err)
	}
}

func TestGoalFileRoundTripPreservesSettings(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "goal.txt"), []byte("First line\nSecond line\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := config.UpdateLocal(root, []config.Override{{Key: "goal_file", Value: "goal.txt"}, {Key: "roster", Value: "ceo"}}); err != nil {
		t.Fatal(err)
	}
	f, err := settingsForm(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.Fields[0].Value != "" || f.Fields[1].Value != "goal.txt" {
		t.Fatal("file goal appears as two conflicting inputs")
	}
	values := []string{}
	for _, v := range f.Fields {
		values = append(values, v.Value)
	}
	if err = saveSettings(root, values); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(config.LocalDir(root), config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err = saveSettings(root, values); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(config.LocalDir(root), config.FileName))
	if string(before) != string(after) {
		t.Fatal("unchanged save rewrote settings")
	}
	cfg, err := config.Load(root)
	if err != nil || cfg.Goal != "First line\nSecond line\n" {
		t.Fatalf("goal changed: %q %v", cfg.Goal, err)
	}
}

func TestFormsRemainVisibleWhileResizingAndErrorsKeepInput(t *testing.T) {
	m := fixture()
	m.Form = &form{Kind: "test", Title: "Public test", Fields: []field{{Label: "Instructions", Value: "Find confusing controls"}, {Label: "File"}}, Editing: true, Cursor: 4}
	for _, size := range [][2]int{{120, 30}, {40, 12}, {80, 24}} {
		m.render(size[0], size[1])
		if m.Form.Fields[0].Value != "Find confusing controls" || m.Form.Cursor != 4 {
			t.Fatal("resize lost input")
		}
	}
	m.Message = "Could not read instructions file"
	a := m.key(key{Name: "save"})
	if a == nil || m.Form == nil {
		t.Fatal("submit lost form before successful save")
	}
}

func TestUnknownTerminalSequenceDoesNotSwallowNextKey(t *testing.T) {
	b := []byte("\x1b[99~t")
	_, n := decodeKey(b, false)
	k, _ := decodeKey(b[n:], false)
	if k.Text != "t" {
		t.Fatal("an unknown escape swallowed the following command")
	}
	k, n = decodeKey([]byte("\x1b[4~t"), false)
	if k.Name != "end" || n != 4 {
		t.Fatal(k, n)
	}
}

func TestDocumentHomeKeepsEmployeeAndStopsFollowing(t *testing.T) {
	m := fixture()
	m.Selected = 12
	m.Detail = "agent"
	m.Scroll = 10
	m.key(key{Name: "home"})
	if m.Selected != 12 || m.Scroll != 0 {
		t.Fatal("Home changed the employee instead of scrolling its document")
	}
	m.switchScreen(5)
	m.key(key{Name: "home"})
	if m.Follow {
		t.Fatal("Home was overridden by log following")
	}
}

func TestChoosersReplaceTypedInput(t *testing.T) {
	m := fixture()
	m.Form = newForm("hire", "Hire", []field{
		{Label: "Employee", Value: "ceo", Kind: kindStatic},
		{Label: "Profession", Value: "developer", Kind: kindChoice, Options: []option{{Value: "designer", Title: "Designer"}, {Value: "developer", Title: "Developer"}, {Value: "tester", Title: "Tester"}}},
		{Label: "Ticks", Kind: kindNumber},
		{Label: "Tick", Value: "20s", Kind: kindDuration, Options: durationOptions()},
		{Label: "Roster", Value: "ceo, developer", Kind: kindMulti, Options: []option{{Value: "ceo"}, {Value: "designer"}, {Value: "developer"}}},
	}, nil)
	f := m.Form
	if f.Selected != 1 {
		t.Fatal("a fixed field was selected")
	}
	m.key(key{Name: "backtab"})
	if f.Selected != 4 {
		t.Fatal("Shift-Tab did not wrap past the fixed field")
	}
	m.key(key{Name: "tab"})
	m.key(key{Name: "right"})
	if f.Fields[1].Value != "tester" {
		t.Fatal(f.Fields[1].Value)
	}
	m.key(key{Text: "d"})
	if f.Picker == nil || f.Picker.Filter != "d" {
		t.Fatal("typing on a choice did not open a filtered chooser")
	}
	m.key(key{Text: "x"})
	items := f.Picker.visible(f.Fields[1])
	if len(items) != 1 || items[0].Value != "dx" || items[0].Title != "use this value" {
		t.Fatalf("typed value not offered: %+v", items)
	}
	m.key(key{Name: "backspace"})
	m.key(key{Name: "down"})
	m.key(key{Name: "enter"})
	if f.Picker != nil || f.Fields[1].Value != "developer" {
		t.Fatalf("chooser did not pick the filtered item: %+v", f.Fields[1].Value)
	}
	m.key(key{Name: "tab"})
	m.key(key{Name: "right"})
	m.key(key{Name: "right"})
	m.key(key{Name: "left"})
	if f.Fields[2].Value != "1" {
		t.Fatal(f.Fields[2].Value)
	}
	m.key(key{Name: "left"})
	if f.Fields[2].Value != "" {
		t.Fatal("zero should read as inherit")
	}
	m.key(key{Text: "a"})
	m.key(key{Text: "7"})
	if f.Fields[2].Value != "7" {
		t.Fatal("number field accepted letters")
	}
	m.key(key{Name: "esc"})
	m.key(key{Name: "tab"})
	m.key(key{Name: "right"})
	if f.Fields[3].Value != "30s" {
		t.Fatal(f.Fields[3].Value)
	}
	m.key(key{Name: "tab"})
	m.key(key{Name: "enter"})
	p := f.Picker
	if p == nil || p.Counts["ceo"] != 1 || p.Counts["designer"] != 0 {
		t.Fatal("roster chooser lost its marks")
	}
	m.key(key{Name: "down"})
	m.key(key{Text: " "})
	m.key(key{Name: "tab"})
	if f.Fields[4].Value != "ceo, developer, designer" {
		t.Fatalf("roster order changed: %q", f.Fields[4].Value)
	}
	for _, size := range [][2]int{{120, 30}, {40, 12}} {
		m.key(key{Name: "enter"})
		for _, r := range m.render(size[0], size[1]) {
			if width(r.Text) > size[0] {
				t.Fatalf("chooser overflow at %v: %q", size, r.Text)
			}
		}
		m.key(key{Name: "esc"})
	}
}

func TestPathChooserBrowsesAndStoresCompanyRelativePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "goal.md"), []byte("goal"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fixture()
	m.Form = newForm("save-settings", "Settings", []field{{Label: "Goal file", Kind: kindFile, Base: root}}, nil)
	m.key(key{Name: "enter"})
	p := m.Form.Picker
	if p == nil || p.Dir != root {
		t.Fatalf("browser did not start at the company: %+v", p)
	}
	m.key(key{Text: "doc"})
	m.key(key{Name: "enter"})
	if p.Dir != filepath.Join(root, "docs") || p.Filter != "" {
		t.Fatalf("Enter did not open the folder: %+v", p)
	}
	m.key(key{Name: "backspace"})
	if p.Dir != root {
		t.Fatal("Backspace did not go to the parent")
	}
	m.key(key{Text: "docs"})
	m.key(key{Name: "enter"})
	m.key(key{Text: "goal"})
	m.key(key{Name: "enter"})
	if m.Form.Picker != nil || m.Form.Fields[0].Value != filepath.Join("docs", "goal.md") {
		t.Fatalf("file not stored relative to the company: %q", m.Form.Fields[0].Value)
	}
	if got := absolute(m.Form.Fields[0].Value, root); got != filepath.Join(root, "docs", "goal.md") {
		t.Fatal(got)
	}
}

func TestSettingsFormSwapsModelChoicesWithHarness(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	if err := config.UpdateLocal(root, []config.Override{{Section: "harness codex", Key: "model", Value: "gpt-5.1-codex-max"}}); err != nil {
		t.Fatal(err)
	}
	f, err := settingsForm(root)
	if err != nil {
		t.Fatal(err)
	}
	if f.Fields[3].Value != "claude" || len(f.Fields[4].Options) < 2 {
		t.Fatalf("no model suggestions for the default harness: %+v", f.Fields[4])
	}
	m := fixture()
	m.Form = f
	f.Selected = 3
	m.key(key{Text: "cod"})
	m.key(key{Name: "enter"})
	if f.Fields[3].Value != "codex" || f.Fields[4].Value != "gpt-5.1-codex-max" {
		t.Fatalf("model did not follow the harness: %+v", f.Fields[4])
	}
	if _, ok := f.Fields[5].option("high"); !ok {
		t.Fatal("effort suggestions missing")
	}
}

func TestPublicTesterSettingsRoundTrip(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	f, err := settingsForm(root)
	if err != nil {
		t.Fatal(err)
	}
	f.Fields[0].Value = "Build a small testable product"
	f.Fields[2].Value = "ceo, developer-1, developer-2"
	f.Fields[3].Value = "omp"
	f.changed(3)
	f.Fields[4].Value = "ollama/weak"
	f.Fields[10].Value = "codex"
	f.changed(10)
	if _, ok := f.Fields[12].option("high"); !ok {
		t.Fatal("reviewer effort choices did not follow its harness")
	}
	f.Fields[11].Value, f.Fields[12].Value = "frontier-reviewer", "high"
	if err := saveSettings(root, f.values()); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root)
	if err != nil || c.User.Harness != "codex" || c.User.Model != "frontier-reviewer" || c.User.Effort != "high" || c.Harnesses["omp"].Model != "ollama/weak" {
		t.Fatalf("settings lost or mixed: %+v, %v", c, err)
	}
	for _, name := range []string{"developer-1", "developer-2"} {
		if _, err := os.Stat(filepath.Join(root, "spaces", name, "role.md")); err != nil {
			t.Fatal(err)
		}
	}
	f, err = settingsForm(root)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(config.LocalDir(root), config.FileName)
	before, _ := os.ReadFile(p)
	if err := saveSettings(root, f.values()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("unchanged save rewrote public tester settings")
	}
	f.Fields[10].Value = "missing-reviewer"
	if err := saveSettings(root, f.values()); err == nil {
		t.Fatal("unknown reviewer harness accepted")
	}
	after, _ = os.ReadFile(p)
	if string(before) != string(after) {
		t.Fatal("invalid reviewer settings were written")
	}
	f.Fields[10].Value = ""
	f.changed(10)
	if err := saveSettings(root, f.values()); err != nil {
		t.Fatal(err)
	}
	c, err = config.Load(root)
	if err != nil || c.User.Harness != "" || c.User.Model != "" || c.User.Effort != "" || c.HarnessFor("") != "omp" {
		t.Fatalf("clearing reviewer overrides did not restore inheritance: %+v, %v", c.User, err)
	}
}

func TestRosterCountsAndToggle(t *testing.T) {
	m := fixture()
	f := newForm("test", "Roster", []field{{Label: "Roster", Kind: kindMulti, Value: "ceo, developer", Options: []option{{Value: "ceo"}, {Value: "developer"}, {Value: "designer"}}}}, nil)
	m.Form = f
	m.key(key{Name: "enter"})
	m.key(key{Text: "9"})
	if f.Picker.Counts["ceo"] != 1 {
		t.Fatal("number key created multiple CEOs")
	}
	m.key(key{Name: "down"})
	m.key(key{Text: "3"})
	if f.Picker.Filter != "" || f.Picker.Counts["developer"] != 3 {
		t.Fatal("digit filtered instead of setting the count")
	}
	m.key(key{Text: "2"}) // replaces, rather than appending to, the previous digit
	m.key(key{Name: "tab"})
	if f.Fields[0].Value != "ceo, developer-1, developer-2" {
		t.Fatalf("wrong numbered roster: %s", f.Fields[0].Value)
	}
	m.key(key{Name: "enter"})
	if f.Picker.Counts["developer"] != 2 || len(f.Picker.Items) != 3 {
		t.Fatal("reopening did not group numbered developers")
	}
	m.key(key{Name: "down"})
	m.key(key{Name: "enter"})
	if f.Picker == nil || f.Picker.Counts["developer"] != 0 {
		t.Fatal("Return did not toggle the profession off")
	}
	m.key(key{Name: "enter"})
	if f.Picker.Counts["developer"] != 1 {
		t.Fatal("Return did not default to one employee")
	}
	m.key(key{Text: "0"})
	m.key(key{Name: "tab"})
	if f.Fields[0].Value != "ceo" {
		t.Fatal("zero did not clear developers")
	}
	// Existing numbering and interleaved ordering survive opening and accepting.
	f.Fields[0].Value = "ceo, developer-2, designer, developer-5"
	m.key(key{Name: "enter"})
	m.key(key{Name: "tab"})
	if f.Fields[0].Value != "ceo, developer-2, designer, developer-5" {
		t.Fatal("unchanged counts renamed existing employees")
	}
	m.key(key{Name: "enter"})
	m.key(key{Name: "down"})
	m.key(key{Text: "9"})
	for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 12}} {
		for _, row := range m.render(size[0], size[1]) {
			if width(row.Text) > size[0] {
				t.Fatalf("roster overflowed %v: %q", size, row.Text)
			}
		}
	}
	m.key(key{Name: "esc"})
	if f.Fields[0].Value != "ceo, developer-2, designer, developer-5" {
		t.Fatal("cancel changed the roster")
	}
}

func TestEditingCursorFollowsTheField(t *testing.T) {
	m := fixture()
	m.Form = newForm("test", "Test", []field{{Label: "Text", Value: strings.Repeat("x", 200)}}, nil)
	m.key(key{Name: "enter"})
	m.key(key{Name: "home"})
	m.render(80, 24)
	if m.CursorX != 5+4 || m.CursorY != 5 {
		t.Fatalf("cursor at %d,%d", m.CursorX, m.CursorY)
	}
	m.key(key{Name: "end"})
	m.render(80, 24)
	if m.CursorX < 40 || m.CursorX >= 80 {
		t.Fatalf("scrolled cursor at %d", m.CursorX)
	}
	m.key(key{Name: "esc"})
	m.render(80, 24)
	if m.CursorX != -1 {
		t.Fatal("cursor shown while not editing")
	}
}
