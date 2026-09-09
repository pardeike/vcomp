package tmux

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSessionLifecycle(t *testing.T) {
	if !Available() {
		t.Skip("tmux not installed")
	}
	const s = "vcomp-selftest"
	_ = Kill(s)
	dir, _ := os.MkdirTemp("", "vcomp")
	defer os.RemoveAll(dir)

	if err := New(s, dir, []string{"sh"}); err != nil {
		t.Fatalf("New: %v", err)
	}
	defer Kill(s)
	if !Exists(s) {
		t.Fatal("session should exist")
	}

	// Typing a line into an interactive shell must reach it and run.
	if err := SendLine(s, "echo hello-from-pane"); err != nil {
		t.Fatalf("SendLine: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	out, err := Capture(s)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !strings.Contains(out, "hello-from-pane") {
		t.Fatalf("pane did not receive the line, got:\n%s", out)
	}

	// A session whose command exits is kept, but reports itself dead, so that
	// whatever the command printed on its way out can still be read.
	if !Alive(s) {
		t.Fatal("session should be alive while its command runs")
	}
	if err := SendLine(s, "exit"); err != nil {
		t.Fatalf("SendLine exit: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	if !Exists(s) {
		t.Fatal("the pane should be kept after the command exited")
	}
	if !Dead(s) || Alive(s) {
		t.Fatal("an exited command must report the session as dead")
	}
	if out, err := Capture(s); err != nil || !strings.Contains(out, "hello-from-pane") {
		t.Fatalf("a dead pane must still be readable, got %q %v", out, err)
	}
	if err := Kill(s); err != nil || Exists(s) {
		t.Fatalf("Kill should remove even a dead session: %v", err)
	}
}

func TestOriginalPaneSurvivesSelectionChanges(t *testing.T) {
	if !Available() {
		t.Skip("tmux unavailable")
	}
	session := fmt.Sprintf("vcomp-pane-test-%d", os.Getpid())
	if err := New(session, t.TempDir(), []string{"/bin/cat"}); err != nil {
		t.Fatal(err)
	}
	defer Kill(session)
	original := Option(session, "@vcomp-pane")
	if _, err := run("split-window", "-t", original, "/bin/cat"); err != nil {
		t.Fatal(err)
	}
	if err := SendLine(session, "original-pane-only"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	out, err := Capture(session)
	if err != nil || !strings.Contains(out, "original-pane-only") {
		t.Fatalf("prompt missed original pane: %q %v", out, err)
	}
	if _, err := run("kill-pane", "-t", original); err != nil {
		t.Fatal(err)
	}
	if !Exists(session) || !Dead(session) {
		t.Fatal("missing original pane was mistaken for a live agent")
	}
}
