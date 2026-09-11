package tui

import (
	"testing"
	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/engine"
)

func TestPersistentIndependentTableSortsRetainSelection(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	a := app{m: model{Root: root, Screen: 0}, refresh: make(chan refreshResult, 1), reading: true}
	for _, tt := range []struct {
		screen           int
		field, direction string
	}{{0, "inbox", "descending"}, {6, "sector", "ascending"}, {1, "created", "descending"}} {
		a.m.Screen = tt.screen
		if err := a.sortAction(action{Kind: "sort-save", Values: []string{tt.field, tt.direction}}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil || cfg.UISort["dashboard_sort"] != "-inbox" || cfg.UISort["catalogue_sort"] != "sector" || cfg.UISort["public_tests_sort"] != "-created" {
		t.Fatal(cfg.UISort, err)
	}
	d := data{View: engine.Observation{Config: cfg, Agents: []engine.AgentView{{Name: "a", Inbox: 1}, {Name: "b", Inbox: 3}}}, Catalogue: []bootstrap.Position{{Name: "a", Sector: "z"}, {Name: "b", Sector: "a"}}}
	m := model{Data: d, Screen: 0}
	sortData(&d)
	m.update(d)
	if d.View.Agents[0].Name != "b" || d.Catalogue[0].Name != "b" {
		t.Fatal("sort not applied")
	}
	// The fixture shares slices; validate refresh preservation with an independent order.
	m.Selected = 1
	d.View.Agents = []engine.AgentView{{Name: "a"}, {Name: "b"}}
	m.update(d)
	if m.agent() != "a" {
		t.Fatal("sort moved role selection")
	}
	if err := config.UpdateLocal(root, []config.Override{{Key: "dashboard_sort", Value: "bogus"}}); err == nil {
		t.Fatal("accepted invalid field")
	}
}
