package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"vcomp/internal/tmux"

	"vcomp/internal/config"
)

func TestObserveEmptyDirectoryIsReadOnly(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	v := Observe(root)
	if v.Exists || v.Supervised {
		t.Fatalf("invented company state: %+v", v)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("observer wrote to root: %v %v", entries, err)
	}
}
func TestObservationDistinguishesStaleLockAndReportsTelemetry(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	if err := e.Lock(); err != nil {
		t.Fatal(err)
	}
	if !Supervising(root) {
		t.Fatal("live lock not observed")
	}
	tick(t, e)
	tick(t, e)
	s := e.st.Roles["ceo"]
	s.Broken = true
	s.Idle = 7
	s.LastErr = "fixture startup error"
	e.saveState()
	v := Observe(root)
	if len(v.Agents) != 1 || v.Agents[0].State != "broken" || v.Agents[0].Idle != 7 || v.ObservedAt.IsZero() {
		t.Fatalf("missing telemetry: %+v", v)
	}
	e.Unlock()
	if err := os.WriteFile(filepath.Join(config.LocalDir(root), "engine.pid"), []byte("999999\nvcomp-lock\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if Supervising(root) {
		t.Fatal("stale pid reported as supervised")
	}
	v = Observe(root)
	if v.Agents[0].State == "broken" {
		t.Fatal("stale failure applied to unsupervised session")
	}
}

func TestObserveReadsTurnsFromOriginalAgentTerminal(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	history := t.TempDir()
	if err := config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "turn_history", Value: history}}); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	pane, err := tmux.Inspect(e.Session("ceo"))
	if err != nil || pane.TTY == "" {
		t.Fatalf("missing terminal: %+v %v", pane, err)
	}
	cwd := filepath.Join(root, "spaces", "ceo")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := fmt.Sprintf("{\"type\":\"session\",\"cwd\":%q}\n", cwd) + turnEntry("user", "", 0) + turnEntry("assistant", "stop", 10)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(history, filepath.Base(pane.TTY)), []byte(cwd+"\n"+path+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	view := Observe(root)
	if len(view.Agents) != 1 || view.Agents[0].Turns.Completed != 1 || view.Agents[0].Turns.Average != 10*time.Second {
		t.Fatalf("missing turn telemetry: %+v", view.Agents)
	}
}

func TestPublicRunCreationAndCompletionObservation(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	dir := filepath.Join(root, "public", "run-0001")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	view := Observe(root)
	if len(view.Runs) != 1 || view.Runs[0].Created.IsZero() || view.Runs[0].State != "waiting" {
		t.Fatal(view.Runs)
	}
	e.st.Runs["run-0001"] = &runState{Attempts: 1}
	e.saveState()
	if got := Observe(root).Runs[0].State; got != "retry pending" {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, "impressions.md"), []byte("Could not finish."), 0644)
	view = Observe(root)
	if view.Runs[0].State != "done" || view.Runs[0].Attempts != 1 {
		t.Fatal(view.Runs)
	}
}
