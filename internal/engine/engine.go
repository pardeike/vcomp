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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

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
	// Failure state is deliberately not persisted: restarting the engine is a
	// person saying "try again", and it should not inherit an old verdict.
	Fails    int    `json:"-"`
	Broken   bool   `json:"-"`
	LastErr  string `json:"-"`
	PaneHash string `json:"-"`
	Idle     int    `json:"-"`
}

type runState struct {
	Attempts   int       `json:"attempts"`
	StartedAt  time.Time `json:"startedAt"`
	NeedShake  bool      `json:"needShake"`
	NeedPrompt bool      `json:"needPrompt"`
	PaneHash   string    `json:"-"`
	Idle       int       `json:"-"`
}

// msgState is what the audit trail remembers about a message still sitting in
// an inbox, so that a deletion can be dated and its content is never lost.
type msgState struct {
	Hash  string    `json:"hash"`
	First time.Time `json:"first"`
}

type state struct {
	Roles    map[string]*roleState `json:"roles"`
	Runs     map[string]*runState  `json:"runs"`
	Messages map[string]*msgState  `json:"messages"`
	AuditSeq int                   `json:"auditSeq"`
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
		st: state{
			Roles:    map[string]*roleState{},
			Runs:     map[string]*runState{},
			Messages: map[string]*msgState{},
		},
		logF: f,
		log:  log.New(io.MultiWriter(os.Stdout, f), "", log.Ltime),
	}
	e.loadState()
	return e, nil
}

func (e *Engine) Close() error { return e.logF.Close() }

// lockPath holds the pid of the engine running this company.
func (e *Engine) lockPath() string {
	return filepath.Join(config.LocalDir(e.root), "engine.pid")
}

// Lock claims this company for this process. Two engines on one company would
// each prod the same sessions and each believe the other's restarts were their
// own, so the second one refuses rather than fighting.
func (e *Engine) Lock() error {
	if b, err := os.ReadFile(e.lockPath()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && alive(pid) {
			return fmt.Errorf("an engine is already running this company (pid %d)\n"+
				"stop it, or run: vcomp stop -root %s", pid, e.root)
		}
	}
	return os.WriteFile(e.lockPath(), fmt.Appendf(nil, "%d\n", os.Getpid()), 0o644)
}

// Unlock releases the claim. A lock left behind by a crash is stale and the
// next engine takes it, since the pid in it is gone.
func (e *Engine) Unlock() { _ = os.Remove(e.lockPath()) }

// alive reports whether a process still exists. Signal 0 checks without sending.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

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
	if e.st.Messages == nil {
		e.st.Messages = map[string]*msgState{}
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
			e.log.Printf("engine stopped. The agents are still running in tmux, "+
				"working unsupervised - 'vcomp run -root %s' picks them back up, "+
				"'vcomp stop -root %s' ends them.", e.root, e.root)
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
	roles, err := space.Roles(e.root)
	if err != nil {
		e.log.Printf("cannot read spaces: %v", err)
		return false
	}
	e.syncRoles(roles)
	e.syncRuns()
	e.audit(roles)
	e.publish(roles)
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

// ResultFile is the name of the file whose existence ends the simulation.
func (e *Engine) ResultFile() string { return e.cfg.ResultFile }

// Session names the tmux session a role runs in.
func (e *Engine) Session(role string) string { return e.cfg.SessionPrefix + "-" + role }

// Stop kills every session this engine owns.
func (e *Engine) Stop() int { return StopPrefix(e.cfg.SessionPrefix + "-") }

// StopPrefix kills every session whose name starts with prefix. With the bare
// "vcomp-" it finds companies whose engine has gone and left its agents
// running, which is otherwise only discoverable by knowing to run "tmux ls".
func StopPrefix(prefix string) int {
	n := 0
	for _, s := range tmux.List() {
		if strings.HasPrefix(s, prefix) {
			_ = tmux.Kill(s)
			n++
		}
	}
	return n
}

// Orphans lists running vcomp sessions that no live engine is supervising.
func Orphans() []string {
	var out []string
	for _, s := range tmux.List() {
		if strings.HasPrefix(s, "vcomp") {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
