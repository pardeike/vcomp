package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vcomp/internal/config"
	"vcomp/internal/tmux"
)

func TestDirectTextRejectsTerminalCommands(t *testing.T) {
	for _, text := range []string{"", "\x1b[2J", "a\x03b", "x\x00y"} {
		if _, err := directText(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	if got, err := directText("/quit\nhello\r\nworld"); err != nil || got != "FROM USER: /quit hello  world" {
		t.Fatal(got, err)
	}
}

func TestInterruptPatternsFromRealFrames(t *testing.T) {
	cfg := config.Default()
	for _, name := range []string{"omp", "pi", "claude", "codex", "opencode"} {
		for _, state := range []string{"busy", "done"} {
			b, err := os.ReadFile(filepath.Join("testdata/readiness", name+"-"+state+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			if got := interruptible(cfg.Harnesses[name], string(b)); got != (state == "busy") {
				t.Errorf("%s %s interruptible=%v", name, state, got)
			}
		}
	}
}

// A terminal with genuine interrupt and input states; raw mode lets Escape
// cancel work without waiting for a newline. It records every submission.
func directCompany(t *testing.T, roster string) (string, *Engine) {
	root, _, e := company(t, roster, "")
	script := filepath.Join(root, "direct-harness.py")
	body := `#!/usr/bin/env python3
import os,sys,tty
tty.setraw(0)
n=0
line=''
def draw(state):
 global n
 n+=1
 sys.stdout.write('\x1b[2J\x1b[H'+state+' '+str(n)+'\r\n');sys.stdout.flush()
def record(text):
 with open('received','a') as f: f.write(text+'\n')
draw('READY')
while True:
 c=os.read(0,1).decode()
 if c=='\x1b':
  record('INTERRUPT');line='';draw('DRAFT' if os.path.exists('restore-input') else 'READY')
 elif c=='\x15':
  record('CLEAR');line='';draw('READY')
 elif c in '\r\n':
  record(line);draw('BUSY' if line=='GO' else 'READY');line=''
 else: line+=c
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	updates := []config.Override{
		{Key: "direct_steer_timeout", Value: "1s"}, {Key: "direct_steer_poll", Value: "20ms"},
		{Section: "harness fake", Key: "start", Value: script},
		{Section: "harness fake", Key: "ready_pattern", Value: `(?m)^READY \d+`},
		{Section: "harness fake", Key: "interrupt_pattern", Value: `(?m)^BUSY \d+`},
		{Section: "harness fake", Key: "interrupt", Value: "Escape"},
	}
	if err := config.UpdateLocal(root, updates); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	tick(t, e)
	return root, e
}

func TestDirectBroadcastInterruptAndQueuedRecovery(t *testing.T) {
	root, e := directCompany(t, "ceo, developer")
	for _, name := range []string{"ceo", "developer"} {
		tmux.SendLine(e.Session(name), "GO")
	}
	pane(t, e.Session("ceo"))
	queued := e.directSteer(DirectRequest{All: true, Mode: "queued", Text: "later"}, nil)
	if len(queued) != 2 || !strings.HasPrefix(queued[0].Status, "queued") {
		t.Fatal(queued)
	}
	next, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	for _, name := range []string{"ceo", "developer"} {
		if len(next.st.Roles[name].DirectPrompts) != 1 {
			t.Fatal("queue lost on restart", name)
		}
	}
	results := next.directSteer(DirectRequest{All: true, Mode: "immediate", Text: "redirect"}, nil)
	for _, r := range results {
		if !strings.HasPrefix(r.Status, "submitted") {
			t.Fatal(results)
		}
	}
	tick(t, next)
	for _, name := range []string{"ceo", "developer"} {
		got := read(t, filepath.Join(root, "spaces", name, "received"))
		want := "FRESHPROMPT\nGO\nINTERRUPT\nFROM USER: redirect\nFROM USER: later\n"
		if got != want {
			t.Fatalf("%s: got %q want %q", name, got, want)
		}
	}
}

func TestImmediateFailureHoldsNudgesAndRetryReleases(t *testing.T) {
	root, e := directCompany(t, "ceo")
	tmux.SendLine(e.Session("ceo"), "GO")
	pane(t, e.Session("ceo"))
	// A no-op interrupt leaves the terminal busy. It must never receive text.
	config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "interrupt", Value: "C-b"}})
	r := e.directSteer(DirectRequest{Role: "ceo", Mode: "immediate", Text: "urgent"}, nil)
	if !strings.HasPrefix(r[0].Status, "failed") || e.st.Roles["ceo"].DirectError == "" {
		t.Fatal(r)
	}
	tmux.SendKeys(e.Session("ceo"), []string{"Escape"})
	pane(t, e.Session("ceo"))
	for i := 0; i < 6; i++ {
		tick(t, e)
	}
	got := read(t, filepath.Join(root, "spaces/ceo/received"))
	if strings.Contains(got, "NUDGEPROMPT") || strings.Contains(got, "urgent") {
		t.Fatal(got)
	}
	r = e.directSteer(DirectRequest{Role: "ceo", Mode: "immediate", Text: "retry"}, nil)
	if !strings.HasPrefix(r[0].Status, "submitted") || e.st.Roles["ceo"].DirectError != "" {
		t.Fatal(r)
	}
}

func TestDirectSocketSerializesWithSupervisor(t *testing.T) {
	root, e := directCompany(t, "ceo")
	if err := e.Lock(); err != nil {
		t.Fatal(err)
	}
	closeControl, err := e.ListenControl()
	if err != nil {
		t.Fatal(err)
	}
	defer closeControl()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); e.Run(stop) }()
	defer func() { close(stop); <-done }()
	r, err := RequestDirect(root, DirectRequest{Role: "ceo", Mode: "immediate", Text: "socket prompt"})
	if err != nil || len(r) != 1 || !strings.HasPrefix(r[0].Status, "submitted") {
		t.Fatal(r, err)
	}
	time.Sleep(100 * time.Millisecond)
	got := read(t, filepath.Join(root, "spaces/ceo/received"))
	if strings.Count(got, "FROM USER: socket prompt") != 1 {
		t.Fatal(got)
	}
	if r, err := RequestDirect(root, DirectRequest{Role: "missing", Mode: "immediate", Text: "bad"}); err != nil || !strings.HasPrefix(r[0].Status, "failed") {
		t.Fatal(fmt.Sprint(r), err)
	}
}

func TestImmediateClearsRecognizedRestoredInput(t *testing.T) {
	root, e := directCompany(t, "ceo")
	os.WriteFile(filepath.Join(root, "spaces/ceo/restore-input"), nil, 0644)
	config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "interrupt_clear", Value: "C-u"}, {Section: "harness fake", Key: "interrupt_input_pattern", Value: `(?m)^DRAFT \d+`}})
	tmux.SendLine(e.Session("ceo"), "GO")
	pane(t, e.Session("ceo"))
	r := e.directSteer(DirectRequest{Role: "ceo", Mode: "immediate", Text: "replacement"}, nil)
	if !strings.HasPrefix(r[0].Status, "submitted") {
		t.Fatal(r)
	}
	got := read(t, filepath.Join(root, "spaces/ceo/received"))
	if !strings.Contains(got, "INTERRUPT\nCLEAR\nFROM USER: replacement\n") {
		t.Fatal(got)
	}
	h := config.Default().Harnesses["claude"]
	b, _ := os.ReadFile("testdata/readiness/claude-restored-input.txt")
	if !restoredInput(h, string(b)) {
		t.Fatal("Claude restored input not recognized")
	}
	b, _ = os.ReadFile("testdata/readiness/claude-busy.txt")
	if restoredInput(h, string(b)) {
		t.Fatal("would clear busy Claude input")
	}
}
