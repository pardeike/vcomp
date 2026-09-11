package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"vcomp/internal/space"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/tmux"
)

const prefix = "vcomptest"

// fakeHarness is a stand-in for claude/codex: it records how it was started and
// then sits on the terminal echoing whatever is typed at it, so a test can see
// which prompts the engine sent.
const fakeHarness = `#!/bin/sh
echo "$1 $PWD" >> LOGFILE
if [ "$1" = resume ] && [ -f LOGFILE.noresume ]; then exit 1; fi
exec cat
`

// company builds a throwaway company wired to the fake harness and returns the
// root, the harness log path, and a ready engine. The global settings
// directory is redirected so the developer's own ~/.vcomp cannot leak in.
func company(t *testing.T, roster string, extraConf string) (string, string, *Engine) {
	t.Helper()
	if !tmux.Available() {
		t.Skip("tmux not installed")
	}
	if os.Getenv("VCOMP_TEST_TMUX") == "" {
		real, err := exec.LookPath("tmux")
		if err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		socket := fmt.Sprintf("vcomp-test-%d", os.Getpid())
		os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexec "+real+" -L "+socket+" \"$@\"\n"), 0755)
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("VCOMP_TEST_TMUX", socket)
	}
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()

	harnessLog := filepath.Join(root, "harness.log")
	script := filepath.Join(root, "fake-harness")
	if err := os.WriteFile(script, []byte(strings.ReplaceAll(fakeHarness, "LOGFILE", harnessLog)), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "session_prefix = " + prefix + "\n" +
		"goal = build something testable\n" +
		"tick = 1s\nidle_ticks = 1\nidle_ticks_empty = 5\n" +
		"user_timeout = 10m\nuser_max_attempts = 2\n" +
		"harness = fake\nroster = " + roster + "\n" +
		"[harness fake]\nstart = " + script + " start\nresume = " + script + " resume\n" +
		"[prompts]\nfresh = FRESHPROMPT\nback = BACKPROMPT\nnudge = NUDGEPROMPT\n" +
		"user = USERPROMPT\nuser_nudge = USERNUDGE\n" + extraConf
	if err := os.MkdirAll(config.LocalDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.LocalDir(root), config.FileName), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	e, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		e.Stop()
		e.Close()
	})
	return root, harnessLog, e
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

// pane waits briefly for the harness to render, then returns the pane text.
func pane(t *testing.T, session string) string {
	t.Helper()
	time.Sleep(400 * time.Millisecond)
	out, err := tmux.Capture(session)
	if err != nil {
		t.Fatalf("capture %s: %v", session, err)
	}
	return out
}

// tick advances the engine, leaving the panes time to settle first so that a
// motionless agent really does look motionless.
func tick(t *testing.T, e *Engine) bool {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	return e.Tick()
}

func TestHireResumeAndReplace(t *testing.T) {
	root, harnessLog, e := company(t, "ceo, developer-1", "")
	ceo, dev := prefix+"-ceo", prefix+"-developer-1"

	// Tick one hires everybody; the prompt deliberately waits for tick two so
	// the harness has had time to draw itself.
	tick(t, e)
	if !tmux.Exists(ceo) || !tmux.Exists(dev) {
		t.Fatal("both roles should have a session after the first tick")
	}
	if n := strings.Count(read(t, harnessLog), "start "); n != 2 {
		t.Fatalf("expected 2 fresh starts, got:\n%s", read(t, harnessLog))
	}

	tick(t, e)
	if got := pane(t, ceo); !strings.Contains(got, "FRESHPROMPT") {
		t.Fatalf("ceo never got the fresh prompt, pane:\n%s", got)
	}

	// A crashed session comes back with the resume command, keeping its memory.
	if err := tmux.Kill(dev); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if !tmux.Exists(dev) {
		t.Fatal("developer-1 should have been revived")
	}
	if log := read(t, harnessLog); !strings.Contains(log, "resume ") {
		t.Fatalf("revival should use the resume command, got:\n%s", log)
	}
	tick(t, e)
	if got := pane(t, dev); !strings.Contains(got, "BACKPROMPT") {
		t.Fatalf("revived role should get the welcome-back prompt, pane:\n%s", got)
	}

	// The CEO firing someone: role.md is rewritten, so the occupant is replaced
	// and must come back with a blank head - start, never resume.
	before := strings.Count(read(t, harnessLog), "start ")
	rolePath := filepath.Join(root, "ceo-instructions.md")
	if err := config.UpdateLocal(root, []config.Override{{Key: "ceo_instructions_file", Value: rolePath}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rolePath, []byte("# ceo\n\nA different person entirely.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if after := strings.Count(read(t, harnessLog), "start "); after != before+1 {
		t.Fatalf("a rewritten role.md must restart without resume (starts %d -> %d)", before, after)
	}
}

// A role with something waiting for it is prodded quickly; a role with an empty
// inbox is left alone for much longer, so it does not fill the time with
// invented personal work.
func TestEmptyInboxIsPacedMoreSlowly(t *testing.T) {
	root, _, e := company(t, "ceo, developer-1", "")
	ceo, dev := prefix+"-ceo", prefix+"-developer-1"

	topic := filepath.Join(root, "spaces", "developer-1", "inbox", "please-fix-this")
	if err := os.MkdirAll(topic, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(topic, "message.md"), []byte("it is broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	tick(t, e) // hire
	tick(t, e) // fresh prompt
	tick(t, e) // record the pane
	tick(t, e) // idle_ticks = 1 reached for the role with mail

	if got := pane(t, dev); !strings.Contains(got, "NUDGEPROMPT") {
		t.Fatalf("a role with a message waiting should have been nudged, pane:\n%s", got)
	}
	if got := pane(t, ceo); strings.Contains(got, "NUDGEPROMPT") {
		t.Fatalf("a role with an empty inbox should still be left alone, pane:\n%s", got)
	}

	// idle_ticks_empty = 5, so it does get there eventually.
	for i := 0; i < 5; i++ {
		tick(t, e)
	}
	if got := pane(t, ceo); !strings.Contains(got, "NUDGEPROMPT") {
		t.Fatalf("an idle role should be nudged at the slower pace, pane:\n%s", got)
	}
}

// Both harnesses ask "do you trust this folder?" the first time they run
// somewhere, and typing a prompt into that dialog answers it wrongly and quits.
// A configured handshake is pressed first, on its own tick.
func TestHandshakeIsSentBeforeTheFirstPrompt(t *testing.T) {
	_, _, e := company(t, "ceo", "[harness fake]\nhandshake = Enter\n")
	ceo := prefix + "-ceo"

	tick(t, e) // hire
	tick(t, e) // handshake, not the prompt
	if got := pane(t, ceo); strings.Contains(got, "FRESHPROMPT") {
		t.Fatalf("the prompt must wait until after the handshake, pane:\n%s", got)
	}
	tick(t, e) // now the prompt
	if got := pane(t, ceo); !strings.Contains(got, "FRESHPROMPT") {
		t.Fatalf("the prompt should follow the handshake, pane:\n%s", got)
	}
}

// A resume that dies means there was nothing to come back to - the usual case
// for a company whose roles have never run under this harness. It must fall
// back to a fresh start every time, and never count towards giving up.
func TestFailedResumeAlwaysFallsBackToAFreshStart(t *testing.T) {
	_, harnessLog, e := company(t, "ceo", "")
	ceo := prefix + "-ceo"

	tick(t, e) // hired, fresh
	tick(t, e) // prompted
	if err := os.WriteFile(harnessLog+".noresume", nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Far more rounds than max_restarts: a failed resume must never accumulate
	// into a verdict on the role.
	for i := 0; i < 4; i++ {
		if err := tmux.Kill(ceo); err != nil {
			t.Fatal(err)
		}
		tick(t, e) // revived with the resume command, which dies
		tick(t, e) // noticed, fell back, started fresh
		if s := e.st.Roles["ceo"]; s.Broken {
			t.Fatalf("round %d: a failed resume must not mark the role broken", i)
		}
		if !tmux.Alive(ceo) {
			t.Fatalf("round %d: the role should be running again after the fallback", i)
		}
	}
	if n := strings.Count(read(t, harnessLog), "start "); n != 5 {
		t.Fatalf("expected one fresh start per failed resume, got %d:\n%s", n, read(t, harnessLog))
	}
}

// Changing a model or an effort must not cost a role its memory: only the CEO
// rewriting role.md ends a living session.
func TestConfigChangesDoNotKillLiveSessions(t *testing.T) {
	root, harnessLog, e := company(t, "ceo", "")
	ceo := prefix + "-ceo"

	tick(t, e)
	tick(t, e)
	starts := strings.Count(read(t, harnessLog), "start ")

	conf := filepath.Join(config.LocalDir(root), config.FileName)
	b, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, append(b, []byte("\n[role ceo]\nidle_ticks = 2\nmodel = something-else\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if !tmux.Alive(ceo) {
		t.Fatal("a settings change must not kill a running session")
	}
	if now := strings.Count(read(t, harnessLog), "start "); now != starts {
		t.Fatalf("the session was restarted (%d -> %d starts) by a settings change", starts, now)
	}
}

// The audit trail has to survive agents deleting their own messages, which is
// the whole difficulty: the engine remembers rather than intercepts.
func TestAuditTrailSurvivesDeletion(t *testing.T) {
	root, _, e := company(t, "ceo, developer-1", "")
	inbox := filepath.Join(root, "spaces", "developer-1", "inbox")
	topic := filepath.Join(inbox, "please-fix-this")
	if err := os.MkdirAll(topic, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "From: ceo\n\nThe lamp does not light. Please look.\n"
	if err := os.WriteFile(filepath.Join(topic, "message.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(topic, "screenshot.png"), []byte("not really a png"), 0o644); err != nil {
		t.Fatal(err)
	}

	tick(t, e)
	tick(t, e) // unchanged: must not be logged twice

	// The recipient deals with it and deletes the folder, as they are told to.
	if err := os.RemoveAll(topic); err != nil {
		t.Fatal(err)
	}
	tick(t, e)

	var events []map[string]any
	raw, err := os.ReadFile(filepath.Join(root, ".vcomp", "messages.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("audit line is not JSON: %v", err)
		}
		events = append(events, ev)
	}
	if len(events) != 2 {
		t.Fatalf("want one sent and one cleared, got %d: %v", len(events), events)
	}

	sent := events[0]
	if sent["event"] != "sent" || sent["to"] != "developer-1" || sent["from"] != "ceo" {
		t.Errorf("wrong sent event: %v", sent)
	}
	if !strings.Contains(sent["text"].(string), "The lamp does not light") {
		t.Error("the message body was not preserved")
	}
	if sent["files"].(float64) != 1 {
		t.Errorf("attachments should be counted, got %v", sent["files"])
	}
	if events[1]["event"] != "cleared" || events[1]["topic"] != "please-fix-this" {
		t.Errorf("wrong cleared event: %v", events[1])
	}
	if events[1]["lived"] == "" {
		t.Error("a cleared message should record how long it sat there")
	}
}

// A per-role override is how a future CEO throttles one person without slowing
// the rest of the company down.
func TestRoleCanBeThrottledIndividually(t *testing.T) {
	_, _, e := company(t, "ceo, developer-1", "[role developer-1]\nidle_ticks_empty = 99\n")
	ceo, dev := prefix+"-ceo", prefix+"-developer-1"

	tick(t, e)
	tick(t, e)
	for i := 0; i < 6; i++ {
		tick(t, e)
	}
	if got := pane(t, ceo); !strings.Contains(got, "NUDGEPROMPT") {
		t.Fatalf("the unthrottled role should have been nudged, pane:\n%s", got)
	}
	if got := pane(t, dev); strings.Contains(got, "NUDGEPROMPT") {
		t.Fatalf("the throttled role should not have been nudged yet, pane:\n%s", got)
	}
}

// The CEO writing the result file is the only way the simulation ends.
func TestResultFileEndsTheSimulation(t *testing.T) {
	root, _, e := company(t, "ceo, developer-1", "")
	ceo := prefix + "-ceo"

	if tick(t, e) {
		t.Fatal("a fresh company is not finished")
	}
	if !tmux.Exists(ceo) {
		t.Fatal("the ceo should be running")
	}
	if _, done := e.Result(); done {
		t.Fatal("there is no result yet")
	}

	answer := "# Done\n\nWe built the thing. The tester verified it.\n"
	if err := os.WriteFile(filepath.Join(root, config.Default().ResultFile), []byte(answer), 0o644); err != nil {
		t.Fatal(err)
	}
	if !e.Tick() {
		t.Fatal("the result file should have ended the simulation")
	}
	got, done := e.Result()
	if !done || got != answer {
		t.Fatalf("Result() = %q, %v; want the CEO's answer", got, done)
	}

	// Run winds the company up and reports that it finished.
	stop := make(chan struct{})
	if !e.Run(stop) {
		t.Fatal("Run should report that the company finished")
	}
	if tmux.Exists(ceo) {
		t.Fatal("every session should be closed once the goal is reached")
	}
}

func TestUserRun(t *testing.T) {
	root, _, e := company(t, "ceo", "")

	runDir := filepath.Join(root, "public", "run-0001")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "instructions.md"), []byte("try it"), 0o644); err != nil {
		t.Fatal(err)
	}

	tick(t, e)
	session := prefix + "-user-run-0001"
	if !tmux.Exists(session) {
		t.Fatal("a pending run should have a user in it")
	}
	for _, f := range []string{"role.md", "version.txt", filepath.Join("product", "README.md")} {
		if _, err := os.Stat(filepath.Join(runDir, f)); err != nil {
			t.Errorf("user run is missing %s: %v", f, err)
		}
	}
	// The snapshot must not carry the repo history: users see the product only.
	if _, err := os.Stat(filepath.Join(runDir, "product", ".git")); err == nil {
		t.Error("the product snapshot should not include .git")
	}

	tick(t, e)
	if got := pane(t, session); !strings.Contains(got, "USERPROMPT") {
		t.Fatalf("user never got prompted, pane:\n%s", got)
	}

	// Writing impressions.md ends the run.
	if err := os.WriteFile(filepath.Join(runDir, "impressions.md"), []byte("it was fine"), 0o644); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if tmux.Exists(session) {
		t.Fatal("the run should end once impressions.md exists")
	}
}

func TestAbandonsRunsThatNeverFinish(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	runDir := filepath.Join(root, "public", "run-0001")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := prefix + "-user-run-0001"
	for i := 0; i < 2; i++ { // user_max_attempts = 2
		tick(t, e)
		if err := tmux.Kill(session); err != nil {
			t.Fatal(err)
		}
	}
	tick(t, e)
	if _, err := os.Stat(filepath.Join(runDir, "abandoned.txt")); err != nil {
		t.Fatalf("run should have been abandoned after 2 attempts: %v", err)
	}
	tick(t, e)
	if tmux.Exists(session) {
		t.Fatal("an abandoned run must not be restarted")
	}
}

func TestPublicTesterUsesSeparateHarnessAndHandshake(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "reviewer.log")
	script := filepath.Join(t.TempDir(), "reviewer")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$* $PWD\" >> "+logPath+"\nexec cat\n"), 0755); err != nil {
		t.Fatal(err)
	}
	root, workerLog, e := company(t, "ceo, developer-1", "[harness frontier]\nstart = "+script+" fresh --model {{model}} --effort {{effort}}\nresume = must-not-resume\nhandshake = Enter\n[user]\nharness = frontier\nmodel = review-model\neffort = high\n")
	runDir := filepath.Join(root, "public", "run-0001")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	session := prefix + "-user-run-0001"
	if !tmux.Exists(session) {
		t.Fatal("reviewer was not started")
	}
	physicalRunDir, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, logPath); !strings.Contains(got, "fresh --model review-model --effort high "+physicalRunDir) {
		t.Fatalf("wrong reviewer command or working directory: %s", got)
	}
	if got := read(t, workerLog); strings.Count(got, "start ") != 2 || strings.Contains(got, "public/") {
		t.Fatalf("employee/reviewer harnesses mixed: %s", got)
	}
	tick(t, e) // reviewer handshake, not the prompt yet
	if got := pane(t, session); strings.Contains(got, "USERPROMPT") {
		t.Fatal("reviewer prompt bypassed its handshake")
	}
	tick(t, e)
	if got := pane(t, session); !strings.Contains(got, "USERPROMPT") {
		t.Fatal("reviewer did not receive its prompt after handshake")
	}
	if err := tmux.Kill(session); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if got := read(t, logPath); strings.Count(got, "fresh --model review-model") != 2 {
		t.Fatalf("reviewer retry must start fresh with the review model: %s", got)
	}
	if err := os.WriteFile(filepath.Join(runDir, "impressions.md"), []byte("Observed the product"), 0644); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if tmux.Exists(session) || !tmux.Exists(prefix+"-ceo") {
		t.Fatal("finishing the public test should stop only its reviewer")
	}
}

func TestResumedSessionKeepsMemoryAfterLaterExit(t *testing.T) {
	_, path, e := company(t, "ceo", "")
	tick(t, e)
	tick(t, e)
	tmux.Kill(e.Session("ceo"))
	tick(t, e)
	tick(t, e)
	tick(t, e)
	before := strings.Count(read(t, path), "start ")
	if err := tmux.SendKeys(e.Session("ceo"), []string{"C-d"}); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if strings.Count(read(t, path), "start ") != before || !e.st.Roles["ceo"].Resumed {
		t.Fatal("successful conversation was restarted fresh")
	}
}

func TestRecoverLiveSessionsWithLostStateAndNewPrefix(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	tick(t, e)
	tick(t, e)
	session := e.Session("ceo")
	os.WriteFile(e.statePath(), []byte("{broken"), 0644)
	if err := config.UpdateLocal(root, []config.Override{{Key: "session_prefix", Value: "changed-prefix"}}); err != nil {
		t.Fatal(err)
	}
	next, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	tick(t, next)
	if next.Session("ceo") != session || !tmux.Alive(session) {
		t.Fatal("live session was not recovered")
	}
	if tmux.Exists("changed-prefix-ceo") {
		t.Fatal("duplicate role was started")
	}
	if next.st.Roles["ceo"].NeedPrompt {
		t.Fatal("recovered session lost startup state")
	}
}

func TestStartupFailuresStopAndRecoverAfterSettingsEdit(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	if err := config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "start", Value: "/bin/sh -c exit"}, {Key: "max_restarts", Value: "2"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		tick(t, e)
	}
	if !e.st.Roles["ceo"].Broken {
		t.Fatal("startup failures were retried forever")
	}
	if err := config.UpdateLocal(root, []config.Override{{Section: "harness fake", Key: "start", Value: "/bin/cat"}}); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if !tmux.Alive(e.Session("ceo")) {
		t.Fatal("edited settings did not revive role")
	}
}

func TestSnapshotPreservesLinksAndFailedCopyCanRetry(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	product := filepath.Join(root, "product")
	os.WriteFile(filepath.Join(product, "asset"), []byte("content"), 0755)
	os.Symlink("asset", filepath.Join(product, "link"))
	dir := filepath.Join(root, "public", "run-0001")
	os.MkdirAll(dir, 0755)
	run := space.Run{Name: "run-0001", Dir: dir}
	os.Rename(product, product+"-away")
	if err := e.prepareRun(run); err == nil {
		t.Fatal("missing product accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "product")); !os.IsNotExist(err) {
		t.Fatal("partial snapshot published")
	}
	os.Rename(product+"-away", product)
	if err := e.prepareRun(run); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "product", "link")
	if target, err := os.Readlink(link); err != nil || target != "asset" {
		t.Fatalf("link not retained: %s %v", target, err)
	}
	if b, err := os.ReadFile(link); err != nil || string(b) != "content" {
		t.Fatal("link no longer works")
	}
}

func TestCompanyLockAndChangeOnlyOutput(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	if err := e.Lock(); err != nil {
		t.Fatal(err)
	}
	defer e.Unlock()
	next, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.Lock(); err == nil {
		t.Fatal("two engines claimed one company")
	}
	e.Unlock()
	if err := next.Lock(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	e.log = log.New(&out, "", 0)
	e.notice("quiet", "ceo: quiet")
	e.notice("quiet", "ceo: quiet")
	e.notice("quiet", "ceo: active")
	if strings.Count(out.String(), "ceo: quiet") != 1 || !strings.Contains(out.String(), "ceo: active") {
		t.Fatal(out.String())
	}
}

func TestMissingGeneratedRoleAndDeletedPublicRun(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	dir := filepath.Join(root, "public", "run-0001")
	os.MkdirAll(dir, 0755)
	tick(t, e)
	tick(t, e)
	ceo := e.Session("ceo")
	rolePath := filepath.Join(root, "spaces/ceo/role.md")
	os.Remove(rolePath)
	os.RemoveAll(dir)
	tick(t, e)
	if !tmux.Alive(ceo) {
		t.Fatal("missing generated document killed a valid occupant")
	}
	if _, err := os.Stat(rolePath); err != nil {
		t.Fatal("missing document was not restored")
	}
	if tmux.Exists(prefix + "-user-run-0001") {
		t.Fatal("deleted user run left an agent")
	}
}

func TestChangedPrefixDoesNotTouchOtherCompany(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	tick(t, e)
	old := e.Session("ceo")
	if err := config.UpdateLocal(root, []config.Override{{Key: "session_prefix", Value: "other-prefix"}}); err != nil {
		t.Fatal(err)
	}
	if err := tmux.New("other-prefix-ceo", t.TempDir(), []string{"/bin/cat"}, map[string]string{"@vcomp-root": "another-company"}); err != nil {
		t.Fatal(err)
	}
	defer tmux.Kill("other-prefix-ceo")
	tick(t, e)
	if e.Session("ceo") != old {
		t.Fatal("prefix edit detached the original session")
	}
	tmux.Kill(old)
	tick(t, e)
	if tmux.Option("other-prefix-ceo", "@vcomp-root") != "another-company" {
		t.Fatal("foreign session replaced")
	}
	if _, err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	if !tmux.Alive("other-prefix-ceo") {
		t.Fatal("stop killed another company")
	}
}

func TestOutputReportsChangesEvenWithoutStateFile(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	e.cfg.StateFile = ""
	roles, err := space.Roles(root)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	e.log = log.New(&out, "", 0)
	e.publish(roles)
	first := out.String()
	e.publish(roles)
	if out.String() != first || !strings.Contains(first, "product: 1 commit") {
		t.Fatalf("unchanged report repeated or product missing: %s", out.String())
	}
	os.MkdirAll(filepath.Join(root, "spaces/ceo/inbox/new-task"), 0755)
	e.publish(roles)
	if strings.Count(out.String(), "ceo: inbox 1") != 1 || strings.Count(out.String(), "product:") != 1 {
		t.Fatal(out.String())
	}
}

func TestOlderUntaggedSessionIsRecovered(t *testing.T) {
	root, _, e := company(t, "ceo", "")
	session := e.Session("ceo")
	if err := tmux.New(session, filepath.Join(root, "spaces/ceo"), []string{"/bin/cat"}); err != nil {
		t.Fatal(err)
	}
	next, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.Session("ceo") != session || !next.st.Roles["ceo"].Started {
		t.Fatal("legacy conversation was not recovered")
	}
	tick(t, next)
	if tmux.Option(session, "@vcomp-root") != next.root {
		t.Fatal("legacy session was not tagged after adoption")
	}
	if _, err := next.Stop(); err != nil {
		t.Fatal(err)
	}
	if tmux.Exists(session) {
		t.Fatal("legacy session survived stop")
	}
}

func TestLegacyPidLockIsNotOverwritten(t *testing.T) {
	_, _, e := company(t, "ceo", "")
	original := fmt.Sprintf("%d\n", os.Getpid())
	os.WriteFile(e.lockPath(), []byte(original), 0644)
	if err := e.Lock(); err == nil {
		t.Fatal("took over a company with a live legacy PID")
	}
	b, _ := os.ReadFile(e.lockPath())
	if string(b) != original {
		t.Fatal("legacy owner record was overwritten")
	}
}
