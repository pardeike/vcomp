// Package engine is the whole simulation loop. It keeps sessions alive, nudges
// stuck ones, honours role.md rewrites, and runs public user tests. It has no
// opinion about the work itself.
package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

type roleState struct {
	RoleHash   string `json:"roleHash"`
	Cmd        string `json:"cmd"`     // what we last started; a change makes resuming impossible
	Started    bool   `json:"started"` // we have run this occupant before, so resume it
	Harness    string `json:"harness"` // a conversation cannot move between harnesses
	NeedShake  bool   `json:"needShake"`
	NeedPrompt bool   `json:"needPrompt"`
	Resumed    bool   `json:"resumed"` // the pending prompt is a welcome-back, not a hello
	Fails      int    `json:"fails"`   // consecutive times the harness exited on us
	Broken     bool   `json:"broken"`  // stop trying until something changes
	LastErr    string `json:"lastErr"` // so a standing complaint is logged once, not every tick
	PaneHash   string `json:"-"`
	Idle       int    `json:"-"`
}

type runState struct {
	Attempts   int       `json:"attempts"`
	StartedAt  time.Time `json:"startedAt"`
	NeedShake  bool      `json:"needShake"`
	NeedPrompt bool      `json:"needPrompt"`
	PaneHash   string    `json:"-"`
	Idle       int       `json:"-"`
}

type state struct {
	Roles map[string]*roleState `json:"roles"`
	Runs  map[string]*runState  `json:"runs"`
}

type Engine struct {
	root string
	cfg  config.Config
	st   state
	log  *log.Logger
	logF *os.File
}

func New(root string) (*Engine, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.LocalDir(root), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(config.LocalDir(root), "engine.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	e := &Engine{
		root: root,
		cfg:  cfg,
		st:   state{Roles: map[string]*roleState{}, Runs: map[string]*runState{}},
		logF: f,
		log:  log.New(io.MultiWriter(os.Stdout, f), "", log.Ltime),
	}
	e.loadState()
	return e, nil
}

func (e *Engine) Close() error { return e.logF.Close() }

func (e *Engine) statePath() string {
	return filepath.Join(config.LocalDir(e.root), "state.json")
}

func (e *Engine) loadState() {
	b, err := os.ReadFile(e.statePath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &e.st)
	if e.st.Roles == nil {
		e.st.Roles = map[string]*roleState{}
	}
	if e.st.Runs == nil {
		e.st.Runs = map[string]*runState{}
	}
}

func (e *Engine) saveState() {
	if b, err := json.MarshalIndent(e.st, "", "  "); err == nil {
		_ = os.WriteFile(e.statePath(), b, 0o644)
	}
}

// reload picks up edits to vcomp.conf between ticks. A broken file keeps the
// last good configuration rather than stopping the company.
func (e *Engine) reload() {
	cfg, err := config.Load(e.root)
	if err != nil {
		e.log.Printf("config not reloaded: %v", err)
		return
	}
	e.cfg = cfg
}

// Run ticks until the goal is declared reached or stop is closed. It reports
// whether the company finished.
func (e *Engine) Run(stop <-chan struct{}) bool {
	e.log.Printf("engine started: root=%s tick=%s harness=%s", e.root, e.cfg.Tick, e.cfg.Harness)
	if e.Tick() {
		return e.close()
	}
	for {
		// The interval is read fresh each time, so editing tick takes effect.
		t := time.NewTimer(e.cfg.Tick)
		select {
		case <-stop:
			t.Stop()
			e.log.Printf("engine stopped (sessions left running; 'vcomp stop' kills them)")
			return false
		case <-t.C:
			if e.Tick() {
				return e.close()
			}
		}
	}
}

// close winds the company up once the goal has been declared reached.
func (e *Engine) close() bool {
	e.log.Printf("goal declared reached; closing %d sessions", e.Stop())
	return true
}

// Tick is one pass over the whole company. It reports whether the CEO has
// declared the goal reached, which ends the simulation.
func (e *Engine) Tick() bool {
	e.reload()
	if _, done := e.Result(); done {
		return true
	}
	e.syncRoles()
	e.syncRuns()
	e.saveState()
	return false
}

// Result returns what the CEO wrote as the company's final answer, if it has.
// Its existence is the only stop condition the engine knows.
func (e *Engine) Result() (string, bool) {
	name := e.cfg.ResultFile
	if name == "" {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(e.root, name))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (e *Engine) syncRoles() {
	roles, err := space.Roles(e.root)
	if err != nil {
		e.log.Printf("cannot read spaces: %v", err)
		return
	}
	present := map[string]bool{}
	for _, r := range roles {
		present[r.Name] = true
		e.syncRole(r)
	}
	for name := range e.st.Roles {
		if !present[name] {
			_ = tmux.Kill(e.cfg.SessionPrefix + "-" + name)
			delete(e.st.Roles, name)
			e.log.Printf("%s: space gone, role removed", name)
		}
	}
}

func (e *Engine) syncRole(r space.Role) {
	s := e.st.Roles[r.Name]
	if s == nil {
		s = &roleState{}
		e.st.Roles[r.Name] = s
	}
	sess := r.Session(e.cfg.SessionPrefix)

	// An unusable configuration is reported once and then waited out, since
	// fixing it has to be enough to bring the role back.
	start, err := e.cfg.CommandFor(r.Name, false)
	if err != nil {
		if s.LastErr != err.Error() {
			s.LastErr = err.Error()
			e.log.Printf("%s: %v", r.Name, err)
		}
		return
	}
	s.LastErr = ""
	cmd := strings.Join(start, " ")

	// Rewriting role.md is the only thing that ends a living session: the CEO
	// replacing the occupant. Everything else - a new model, a new effort, a
	// different harness - waits until the session is gone anyway, because the
	// history in a running session is the whole point of keeping it running.
	if s.RoleHash != "" && s.RoleHash != r.Hash {
		e.log.Printf("%s: role.md rewritten - occupant replaced", r.Name)
		_ = tmux.Kill(sess)
		*s = roleState{}
	}
	s.RoleHash = r.Hash

	harness := e.cfg.HarnessFor(r.Name)
	if s.Harness != "" && s.Harness != harness {
		s.Started = false // there is no conversation to resume in a new harness
	}
	if s.Cmd != "" && s.Cmd != cmd {
		s.Fails, s.Broken = 0, false // the command changed, so it deserves another go
	}
	s.Harness, s.Cmd = harness, cmd

	// A pane whose command exited is kept, so we can say why it died instead of
	// restarting it forever in silence.
	if tmux.Exists(sess) && tmux.Dead(sess) {
		e.roleDied(r.Name, s, sess)
	}
	if s.Broken {
		return
	}

	if !tmux.Exists(sess) {
		e.hire(r, s)
		return
	}
	// Surviving a whole tick is what counts as working.
	s.Fails = 0

	// Answer whatever the harness asks before it will listen, one tick before
	// the prompt, so the dialog has settled.
	if s.NeedShake {
		_ = tmux.SendKeys(sess, e.cfg.Handshake(r.Name))
		s.NeedShake = false
		return
	}

	if s.NeedPrompt {
		kind := config.PromptFresh
		if s.Resumed {
			kind = config.PromptBack
		}
		e.send(sess, e.cfg.Prompt(r.Name, kind))
		s.NeedPrompt, s.Idle, s.PaneHash = false, 0, ""
		return
	}

	// A pane whose text has not changed at all for several ticks is stuck: a
	// working agent always animates something.
	pane, err := tmux.Capture(sess)
	if err != nil {
		return
	}
	if h := space.Hash([]byte(pane)); h == s.PaneHash {
		s.Idle++
	} else {
		s.PaneHash, s.Idle = h, 0
	}
	// Someone with nothing in their inbox is left alone for longer.
	empty := inboxEmpty(r)
	if threshold := e.cfg.IdleThreshold(r.Name, empty); s.Idle >= threshold {
		// Not logged: nudging is the engine's normal heartbeat, not an event.
		e.send(sess, e.cfg.Prompt(r.Name, config.PromptNudge))
		s.Idle, s.PaneHash = 0, ""
	}
}

// hire starts a role's session, resuming its previous conversation if it has one.
func (e *Engine) hire(r space.Role, s *roleState) {
	resume := s.Started
	cmd, err := e.cfg.CommandFor(r.Name, resume)
	if err != nil {
		e.log.Printf("%s: %v", r.Name, err)
		return
	}
	if err := tmux.New(r.Session(e.cfg.SessionPrefix), r.Dir, cmd); err != nil {
		e.log.Printf("%s: cannot start session: %v", r.Name, err)
		return
	}
	verb := "hired"
	if resume {
		verb = "revived"
	}
	e.log.Printf("%s: %s", r.Name, verb)
	s.Started, s.NeedPrompt, s.Resumed = true, true, resume
	s.NeedShake = len(e.cfg.Handshake(r.Name)) > 0
	s.Idle, s.PaneHash = 0, ""
}

// roleDied handles a harness that exited. The first failure after a resume is
// almost always "there was nothing to resume", so it retries from scratch;
// repeated failures mean the command itself is wrong, and hammering it every
// tick only buries the reason in the log.
func (e *Engine) roleDied(name string, s *roleState, sess string) {
	out, _ := tmux.Capture(sess)
	_ = tmux.Kill(sess)
	s.Fails++
	s.NeedShake, s.NeedPrompt = false, false

	switch {
	case s.Resumed && s.Fails == 1:
		e.log.Printf("%s: nothing to resume, starting fresh", name)
		s.Started, s.Resumed = false, false
	case s.Fails >= e.cfg.MaxRestarts:
		s.Broken = true
		e.log.Printf("%s: harness exited %d times, giving up until role.md or the config changes\n"+
			"    command: %s\n%s", name, s.Fails, s.Cmd, indent(lastLines(out, 8)))
	case s.Fails == 1:
		e.log.Printf("%s: session exited, restarting", name)
	}
}

// lastLines returns the tail of a dead pane: the part that says what went wrong.
func lastLines(out string, n int) []string {
	var kept []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimRight(l, " "))
		}
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return kept
}

func indent(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("    | " + l + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// inboxEmpty reports whether anyone has asked this role for anything.
func inboxEmpty(r space.Role) bool {
	entries, err := os.ReadDir(r.Inbox())
	return err != nil || len(entries) == 0
}

func (e *Engine) send(session, text string) {
	if text == "" {
		return
	}
	if err := tmux.SendLine(session, text); err != nil {
		e.log.Printf("%s: cannot send prompt: %v", session, err)
	}
}

func (e *Engine) syncRuns() {
	for _, run := range space.Runs(e.root) {
		rs := e.st.Runs[run.Name]
		if rs == nil {
			rs = &runState{}
			e.st.Runs[run.Name] = rs
		}
		sess := run.Session(e.cfg.SessionPrefix)
		if tmux.Exists(sess) && tmux.Dead(sess) {
			_ = tmux.Kill(sess) // the user's agent exited; the pane was kept
		}
		alive := tmux.Alive(sess)

		if run.Done() {
			if alive {
				_ = tmux.Kill(run.Session(e.cfg.SessionPrefix))
				e.log.Printf("%s: user finished, impressions.md written", run.Name)
			}
			continue
		}
		if run.GivenUp() {
			if alive {
				_ = tmux.Kill(run.Session(e.cfg.SessionPrefix))
			}
			continue
		}
		if alive {
			e.pokeUser(run, rs)
			continue
		}
		if rs.Attempts >= e.cfg.UserMaxAttempts {
			_ = os.WriteFile(run.Abandoned(),
				fmt.Appendf(nil, "no impressions.md after %d attempts\n", rs.Attempts), 0o644)
			e.log.Printf("%s: abandoned after %d attempts", run.Name, rs.Attempts)
			continue
		}
		if err := e.prepareRun(run); err != nil {
			e.log.Printf("%s: cannot prepare: %v", run.Name, err)
			continue
		}
		// Users never resume: every run is someone who has never seen this before.
		cmd, err := e.cfg.CommandFor("", false)
		if err != nil {
			e.log.Printf("%s: %v", run.Name, err)
			continue
		}
		if err := tmux.New(run.Session(e.cfg.SessionPrefix), run.Dir, cmd); err != nil {
			e.log.Printf("%s: cannot start user: %v", run.Name, err)
			continue
		}
		rs.Attempts++
		rs.StartedAt = time.Now()
		rs.NeedPrompt, rs.Idle, rs.PaneHash = true, 0, ""
		rs.NeedShake = len(e.cfg.Handshake("")) > 0
		e.log.Printf("%s: user run started (attempt %d)", run.Name, rs.Attempts)
	}
}

// pokeUser drives a live user session: prompt it, nudge it if it stalls, and
// kill it if it overruns.
func (e *Engine) pokeUser(run space.Run, rs *runState) {
	if rs.NeedShake {
		_ = tmux.SendKeys(run.Session(e.cfg.SessionPrefix), e.cfg.Handshake(""))
		rs.NeedShake = false
		return
	}
	if rs.NeedPrompt {
		e.send(run.Session(e.cfg.SessionPrefix), e.cfg.Prompt("", config.PromptUser))
		rs.NeedPrompt, rs.Idle, rs.PaneHash = false, 0, ""
		return
	}
	if time.Since(rs.StartedAt) > e.cfg.UserTimeout {
		_ = tmux.Kill(run.Session(e.cfg.SessionPrefix))
		e.log.Printf("%s: user run timed out", run.Name)
		return
	}
	pane, err := tmux.Capture(run.Session(e.cfg.SessionPrefix))
	if err != nil {
		return
	}
	if h := space.Hash([]byte(pane)); h == rs.PaneHash {
		rs.Idle++
	} else {
		rs.PaneHash, rs.Idle = h, 0
	}
	if rs.Idle >= e.cfg.IdleThreshold("", false) {
		e.send(run.Session(e.cfg.SessionPrefix), e.cfg.Prompt("", config.PromptUserNudge))
		rs.Idle, rs.PaneHash = 0, ""
	}
}

// prepareRun gives a user run its instructions and a snapshot of the product to
// use. A retried run keeps the snapshot it already has.
func (e *Engine) prepareRun(run space.Run) error {
	rolePath := filepath.Join(run.Dir, space.RoleFile)
	if _, err := os.Stat(rolePath); err != nil {
		doc, err := bootstrap.Load(e.root).Text("user_role.md", nil)
		if err != nil {
			return err
		}
		if err := os.WriteFile(rolePath, []byte(doc), 0o644); err != nil {
			return err
		}
	}
	dst := filepath.Join(run.Dir, space.ProductDir)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	src := filepath.Join(e.root, space.ProductDir)
	if err := copyTree(src, dst); err != nil {
		return err
	}
	version := "unknown"
	if out, err := exec.Command("git", "-C", src, "log", "-1", "--format=%h %s").Output(); err == nil {
		version = strings.TrimSpace(string(out))
	}
	return os.WriteFile(filepath.Join(run.Dir, "version.txt"), []byte(version+"\n"), 0o644)
}

// copyTree copies src to dst, leaving .git behind: the user gets the product,
// not its history.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, info.Mode().Perm())
	})
}

// ResultFile is the name of the file whose existence ends the simulation.
func (e *Engine) ResultFile() string { return e.cfg.ResultFile }

// Session names the tmux session a role runs in.
func (e *Engine) Session(role string) string { return e.cfg.SessionPrefix + "-" + role }

// Stop kills every session this engine owns.
func (e *Engine) Stop() int {
	n := 0
	for _, s := range tmux.List() {
		if strings.HasPrefix(s, e.cfg.SessionPrefix+"-") {
			_ = tmux.Kill(s)
			n++
		}
	}
	return n
}

// Status renders a one-screen view of the company.
func (e *Engine) Status(w io.Writer) {
	fmt.Fprintf(w, "root %s  harness %s  tick %s\n", e.root, e.cfg.Harness, e.cfg.Tick)
	if _, done := e.Result(); done {
		fmt.Fprintf(w, "\nFINISHED: the CEO declared the goal reached in %s\n", e.cfg.ResultFile)
	}
	fmt.Fprintf(w, "\nROLES\n")
	roles, err := space.Roles(e.root)
	if err != nil || len(roles) == 0 {
		fmt.Fprintf(w, "  no company here yet - create one with: vcomp run -root %s -goal \"...\"\n", e.root)
		return
	}
	for _, r := range roles {
		status := "stopped"
		if tmux.Alive(r.Session(e.cfg.SessionPrefix)) {
			status = "running"
		}
		n := 0
		if entries, err := os.ReadDir(r.Inbox()); err == nil {
			n = len(entries)
		}
		harness := ""
		if cmd, err := e.cfg.CommandFor(r.Name, false); err == nil && len(cmd) > 0 {
			harness = cmd[0]
		}
		if s := e.st.Roles[r.Name]; s != nil && s.Broken {
			status = "broken"
		}
		fmt.Fprintf(w, "  %-16s %-8s inbox:%-3d %s\n", r.Name, status, n, harness)
	}

	fmt.Fprintf(w, "\nPUBLIC RUNS\n")
	runs := space.Runs(e.root)
	if len(runs) == 0 {
		fmt.Fprintf(w, "  none\n")
		return
	}
	for _, run := range runs {
		status := "pending"
		switch {
		case run.Done():
			status = "impressions written"
		case run.GivenUp():
			status = "abandoned"
		case tmux.Alive(run.Session(e.cfg.SessionPrefix)):
			status = "user in session"
		}
		fmt.Fprintf(w, "  %-12s %s\n", run.Name, status)
	}
}
