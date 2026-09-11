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
