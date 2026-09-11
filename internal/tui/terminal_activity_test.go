package tui

import (
	"strings"
	"testing"
	"vcomp/internal/config"
	"vcomp/internal/engine"
)

func TestTerminalModesAndInterventionBoundary(t *testing.T) {
	a := engine.AgentView{Name: "hr", Session: "test-hr", Output: "⎋ Reading current manifest\n2h > Model > context", Turns: engine.TurnStats{Known: true, Activity: "Tool: bash", Detail: "Tool: bash\ncat Package.swift"}}
	if got := terminalActivity(a, "brief"); !strings.Contains(got, "Reading current manifest") || strings.Contains(got, "cat Package.swift") {
		t.Fatal(got)
	}
	if got := terminalActivity(a, "detailed"); !strings.Contains(got, "cat Package.swift") {
		t.Fatal(got)
	}
	m := model{Screen: 0, Detail: "agent", Sub: 2, Data: data{View: engine.Observation{Agents: []engine.AgentView{a}}}}
	if act := m.key(key{Text: "v"}); act == nil || act.Kind != "terminal-verbosity" {
		t.Fatal(act)
	}
	if act := m.key(key{Text: "a"}); act == nil || act.Kind != "intervene-confirm" {
		t.Fatal(act)
	}
	t.Setenv(config.HomeEnv, t.TempDir())
	app := app{m: model{Root: t.TempDir()}, reading: true}
	if _, err := app.dispatch(action{Kind: "terminal-verbosity"}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(app.m.Root)
	if err != nil || c.TerminalView != "detailed" {
		t.Fatal(c.TerminalView, err)
	}
}

func TestTerminalFollowsChronologicallyAndFreezesWhileReading(t *testing.T) {
	m := model{Screen: 0, Detail: "agent", Sub: 2, Follow: true, Data: data{View: engine.Observation{Agents: []engine.AgentView{{Name: "hr", Turns: engine.TurnStats{Known: true, Activity: strings.Repeat("Earlier record\n", 40) + "Newest record\n"}}}}}}
	m.renderDocument(80, 20)
	if m.Scroll == 0 {
		t.Fatal("not following bottom")
	}
	m.key(key{Name: "up"})
	frozen := m.document()
	m.Data.View.Agents[0].Turns.Activity += "New arrival\n"
	if m.Follow || m.document() != frozen {
		t.Fatal("incoming output moved paused view")
	}
	m.key(key{Text: "f"})
	if !m.Follow || !strings.Contains(m.document(), "New arrival") {
		t.Fatal("follow did not resume")
	}
	rows := m.renderDocument(80, 20)
	text := ""
	for _, r := range rows {
		text += r.Text + "\n"
	}
	if !strings.Contains(text, "Terminal · brief") || !strings.Contains(text, "New arrival") {
		t.Fatal("status or latest record missing", text)
	}
}

func TestDirectSteerAndBroadcastActions(t *testing.T) {
	m := fixture()
	m.Selected = 3
	if act := m.key(key{Text: "i"}); act == nil || act.Kind != "direct-steer-form" || act.Values[0] != "developer-03" {
		t.Fatal(act)
	}
	if act := m.key(key{Text: "B"}); act == nil || act.Kind != "broadcast-form" {
		t.Fatal(act)
	}
	m.Detail = "agent"
	if act := m.key(key{Text: "B"}); act != nil {
		t.Fatal("broadcast must belong to dashboard", act)
	}
	if act := m.key(key{Text: "i"}); act == nil || act.Kind != "direct-steer-form" {
		t.Fatal(act)
	}
	a := app{m: m}
	if _, err := a.dispatch(action{Kind: "direct-steer-form", Values: []string{"ceo"}}); err != nil {
		t.Fatal(err)
	}
	if a.m.Form.Fields[1].Value != "queued" {
		t.Fatal("default is not queued")
	}
	if _, err := a.dispatch(action{Kind: "broadcast-form"}); err != nil {
		t.Fatal(err)
	}
	if a.m.Form.Fields[0].Value != "All running employees" {
		t.Fatal("not a broadcast")
	}
}

func TestDashboardActivityDoesNotShowInputBorder(t *testing.T) {
	a := engine.AgentView{Harness: "omp", Output: "  ⎋ Reading the project manifest\n ⠇ 2h > Qwen > context\n╰─\n"}
	if got := dashboardActivity(a); got != "Reading the project manifest" {
		t.Fatal(got)
	}
	a.Turns.Activity = "12:00:00  Tool: read\n\n12:01:00  Result: read: Package.swift\n\n"
	if got := dashboardActivity(a); got != "Result: read: Package.swift" {
		t.Fatal(got)
	}
	a = engine.AgentView{Output: "╰─\n ───── \n"}
	if got := dashboardActivity(a); got != "—" {
		t.Fatal(got)
	}
	m := fixture()
	m.Data.View.Agents[0] = engine.AgentView{Name: "ceo", Harness: "omp", Output: "╰─", Turns: engine.TurnStats{Activity: "13:00:00  Tool: read"}}
	text := draw(m.render(240, 45), false)
	if !strings.Contains(text, "Activity") || !strings.Contains(text, "Tool: read") || strings.Contains(text, "Last line") {
		t.Fatal(text)
	}
}
