package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/engine"
	"vcomp/internal/tmux"
)

func TestPlainModeAndInteractiveRouting(t *testing.T) {
	args, plain := plainArgs([]string{"-root", "/tmp/company", "--plain"})
	if !plain || !reflect.DeepEqual(args, []string{"-root", "/tmp/company"}) {
		t.Fatal(args, plain)
	}
	for _, cmd := range []string{"start", "run", "status", "setup", "roles", "hire", "steer", "reset", "stop", "user-run"} {
		if !wantsTUI(cmd, []string{"-root", "/tmp/company"}) {
			t.Errorf("%s should open its screen", cmd)
		}
	}
	for _, tt := range []struct {
		cmd  string
		args []string
	}{{"hire", []string{"developer", "-backstory", "test"}}, {"reset", []string{"-y"}}, {"stop", []string{"-all"}}, {"user-run", []string{"-text", "test"}}, {"attach", []string{"ceo"}}, {"install", nil}} {
		if wantsTUI(tt.cmd, tt.args) {
			t.Errorf("fully specified command must remain immediate: %s %v", tt.cmd, tt.args)
		}
	}
}

func TestTUIWorkflowInRealTerminal(t *testing.T) {
	isolated(t)
	bin := filepath.Join(t.TempDir(), "vcomp")
	build := exec.Command("go", "build", "-o", bin, ".")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, b)
	}
	root := t.TempDir()
	cfg := config.Default()
	cfg.Goal = "Test the dashboard"
	cfg.Roster = []string{"ceo", "developer"}
	cfg.Harness = "fake"
	cfg.Harnesses["fake"] = config.Harness{Start: []string{"/bin/cat"}, Resume: []string{"/bin/cat"}}
	if err := config.UpdateLocal(root, []config.Override{{Key: "goal", Value: cfg.Goal}, {Key: "roster", Value: "ceo, developer"}, {Key: "harness", Value: "fake"}, {Key: "tick", Value: "100ms"}, {Key: "tui_refresh", Value: "100ms"}, {Key: "session_prefix", Value: "tui-workflow"}, {Section: "harness fake", Key: "start", Value: "/bin/cat"}, {Section: "harness fake", Key: "resume", Value: "/bin/cat"}}); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	editor := filepath.Join(root, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\necho visited > \""+filepath.Join(root, "editor-visited")+"\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "terminal.sh")
	text := "#!/bin/sh\nstty -g > before\nEDITOR=\"" + editor + "\" \"" + bin + "\" tui -root \"" + root + "\"\nstty -g > after\necho RESTORED\nexec /bin/cat\n"
	if err := os.WriteFile(script, []byte(text), 0755); err != nil {
		t.Fatal(err)
	}
	if err := tmux.New("tui-view", root, []string{"/bin/sh", script}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command(bin, "stop", "-root", root, "--plain").Run(); tmux.Kill("tui-view") })
	if b, err := exec.Command("tmux", "resize-window", "-t", "tui-view", "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize: %s %v", b, err)
	}
	wait := func(label string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for !predicate() {
			if time.Now().After(deadline) {
				out, _ := tmux.Capture("tui-view")
				t.Fatalf("%s timed out\n%s", label, out)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	has := func(s string) bool { out, _ := tmux.Capture("tui-view"); return strings.Contains(out, s) }
	keys := func(ks ...string) {
		t.Helper()
		if err := tmux.SendKeys("tui-view", ks); err != nil {
			t.Fatal(err)
		}
	}
	typeText := func(s string) {
		t.Helper()
		if b, err := exec.Command("tmux", "send-keys", "-t", "tui-view", "-l", s).CombinedOutput(); err != nil {
			t.Fatalf("type: %s %v", b, err)
		}
	}
	wait("initial dashboard", func() bool { return has("Dashboard") && has("developer") })
	keys("h")
	wait("hire form suggests the next free name", func() bool { return has("Hire an employee") && has("developer-2") })
	keys("Down", "Right") // the name follows the profession until typed over
	wait("suggested name follows profession", func() bool { return has("Hire an employee") && !has("developer-2") })
	keys("Enter")
	typeText("dev")
	wait("profession chooser filters", func() bool { return has("filter dev") })
	keys("Enter", "Up", "Enter", "C-u")
	typeText("developer-2")
	keys("C-s")
	wait("hire", func() bool {
		_, err := os.Stat(filepath.Join(root, "spaces", "developer-2", "role.json"))
		return err == nil
	})
	wait("saved hire", func() bool { return has("hired developer-2") && has("   developer-2") })
	keys("End", "t")
	wait("steer form", func() bool { return has("Steer developer-2") })
	keys("Enter") // the employee line is fixed; the first editable field is selected
	typeText("Focus on the controls.")
	keys("C-s")
	wait("steering", func() bool {
		b, _ := os.ReadFile(filepath.Join(root, "spaces", "developer-2", "role.json"))
		return strings.Contains(string(b), "Focus on the controls.")
	})
	wait("steering saved", func() bool { return has("updated developer-2") })
	keys("n")
	wait("test form", func() bool { return has("Create a public") })
	keys("Enter")
	typeText("Try the controls.")
	keys("C-s")
	wait("test file", func() bool {
		b, _ := os.ReadFile(filepath.Join(root, "public", "run-0001", "instructions.md"))
		return string(b) == "Try the controls."
	})
	wait("test saved", func() bool { return has("run-0001 created") || has("run-0001") && !has("Create a public") })
	keys("4", "e")
	wait("editor returned", func() bool {
		_, err := os.Stat(filepath.Join(root, "editor-visited"))
		return err == nil && has("Settings")
	})
	keys("1", "s")
	wait("supervisor", func() bool { return engine.Supervising(root) && tmux.Alive("tui-workflow-developer-2") })
	for _, size := range [][2]int{{40, 12}, {80, 24}, {140, 42}} {
		exec.Command("tmux", "resize-window", "-t", "tui-view", "-x", fmt.Sprint(size[0]), "-y", fmt.Sprint(size[1])).Run()
		expected := fmt.Sprintf("%dx%d", size[0], size[1])
		wait("resize "+expected, func() bool { return has(expected) })
	}
	keys("r")
	wait("reset confirmation", func() bool { return has("Confirm action") })
	keys("n")
	if _, err := os.Stat(filepath.Join(root, "spaces", "developer-2")); err != nil {
		t.Fatal("cancelled reset removed an employee")
	}
	keys("q")
	wait("exit confirmation", func() bool { return has("supervisor will stop") })
	keys("y")
	wait("terminal restored", func() bool { _, err := os.Stat(filepath.Join(root, "after")); return err == nil })
	before, _ := os.ReadFile(filepath.Join(root, "before"))
	after, _ := os.ReadFile(filepath.Join(root, "after"))
	if string(before) != string(after) {
		t.Fatalf("terminal mode not restored: %q -> %q", before, after)
	}
	if engine.Supervising(root) {
		t.Fatal("owned supervisor survived exit")
	}
	if !tmux.Alive("tui-workflow-ceo") {
		t.Fatal("leaving dashboard killed the agent")
	}
}
