package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vcomp/internal/config"
	"vcomp/internal/tmux"
)

func TestHarnessReadinessFromRealTerminalFrames(t *testing.T) {
	entries, err := os.ReadDir("testdata/readiness")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		name, mode, _ := strings.Cut(strings.TrimSuffix(entry.Name(), ".txt"), "-")
		frame, err := os.ReadFile(filepath.Join("testdata/readiness", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		want := mode == "idle" || mode == "done" || mode == "interrupted"
		if got := inputReady(cfg.Harnesses[name], string(frame)); got != want {
			t.Errorf("%s readiness=%v, want %v", entry.Name(), got, want)
		}
	}
	for name, h := range cfg.Harnesses {
		if inputReady(h, "unchanged unknown terminal") {
			t.Errorf("%s accepted unknown output", name)
		}
	}
	if inputReady(config.Harness{}, "anything") {
		t.Fatal("unconfigured harness accepted input")
	}
}

func TestFrozenBusyPaneDoesNotReceiveEmployeeOrPublicNudges(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	tick(t, e)
	tick(t, e)
	busy := "(?s).*BUSY.*"
	if err := config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "busy_pattern", Value: busy}}); err != nil {
		t.Fatal(err)
	}
	if err := tmux.SendLine(e.Session("ceo"), "BUSY"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		tick(t, e)
	}
	if strings.Contains(pane(t, e.Session("ceo")), "NUDGEPROMPT") {
		t.Fatal("queued employee nudge while busy")
	}
	runDir := filepath.Join(root, "public", "run-0001")
	os.MkdirAll(runDir, 0755)
	tick(t, e)
	tick(t, e)
	sess := prefix + "-user-run-0001"
	tmux.SendLine(sess, "BUSY")
	for i := 0; i < 4; i++ {
		tick(t, e)
	}
	if strings.Contains(pane(t, sess), "USERNUDGE") {
		t.Fatal("queued public tester nudge while busy")
	}
}

func TestInitialPromptWaitsForRecognizedInput(t *testing.T) {
	_, _, e := company(t, "ceo", "[harness fake]\nready_pattern = INPUT-READY\n")
	tick(t, e)
	for i := 0; i < 3; i++ {
		tick(t, e)
	}
	if strings.Contains(pane(t, e.Session("ceo")), "FRESHPROMPT") {
		t.Fatal("initial prompt sent before readiness")
	}
	tmux.SendLine(e.Session("ceo"), "INPUT-READY")
	tick(t, e)
	if !strings.Contains(pane(t, e.Session("ceo")), "FRESHPROMPT") {
		t.Fatal("initial prompt not sent after readiness")
	}
}

func TestUnacknowledgedReadyFrameDoesNotReceiveDuplicatePrompts(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	received := filepath.Join(root, "received")
	script := filepath.Join(root, "unacknowledging-harness")
	text := "#!/bin/sh\nstty -echo\necho INPUT-READY\nwhile IFS= read -r line; do printf '%s\\n' \"$line\" >> '" + received + "'; done\n"
	if err := os.WriteFile(script, []byte(text), 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "start", Value: script}, {Section: "harness fake", Key: "ready_pattern", Value: "INPUT-READY"}}); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	tick(t, e)
	for i := 0; i < 7; i++ {
		tick(t, e)
	}
	b, err := os.ReadFile(received)
	if err != nil || strings.TrimSpace(string(b)) != "FRESHPROMPT" {
		t.Fatalf("duplicate submission: %q %v", b, err)
	}
	next, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.st.Roles["ceo"].LastReady == "" {
		t.Fatal("submission latch not recovered")
	}
	for i := 0; i < 7; i++ {
		tick(t, next)
	}
	b, _ = os.ReadFile(received)
	if strings.TrimSpace(string(b)) != "FRESHPROMPT" {
		t.Fatalf("duplicate submission after supervisor restart: %q", b)
	}
}
