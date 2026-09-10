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
