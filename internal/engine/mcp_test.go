package engine

import (
	"os"
	"path/filepath"
	"testing"

	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

func TestOptionalMCPPreparationFailureStillStartsEmployee(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	e.cfg.Harness = "omp"
	e.cfg.Harnesses["omp"] = e.cfg.Harnesses["fake"]
	e.cfg.MCPEnabled = true
	dir := filepath.Join(root, "spaces", "ceo", ".omp")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("unfinished user edit {"), 0600); err != nil {
		t.Fatal(err)
	}
	roles, err := space.Roles(root)
	if err != nil {
		t.Fatal(err)
	}
	s := &roleState{}
	e.hire(roles[0], s)
	t.Cleanup(func() { tmux.Kill(e.Session("ceo")) })
	pane, err := tmux.Inspect(s.Session)
	if err != nil || !pane.Exists || s.LastErr != "" || s.Cmd == "" {
		t.Fatalf("employee failed to launch: %+v %+v %v", s, pane, err)
	}
}
