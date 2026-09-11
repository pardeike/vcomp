// Package engine is the whole simulation loop. It keeps sessions alive, nudges
// stuck ones, honours role.md rewrites, and runs public user tests. It has no
// opinion about the work itself.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

type roleState struct {
	DirectError   string   `json:"directError,omitempty"`
	DirectPrompts []string `json:"directPrompts,omitempty"`
	LastReady     string   `json:"lastReady,omitempty"`
	Session       string   `json:"session"`
	SettingsHash  string   `json:"settingsHash"`
	RoleHash      string   `json:"roleHash"`
	Cmd           string   `json:"cmd"`     // what we last started; a change makes resuming impossible
	Started       bool     `json:"started"` // we have run this occupant before, so resume it
	Harness       string   `json:"harness"` // a conversation cannot move between harnesses
	NeedShake     bool     `json:"needShake"`
	NeedPrompt    bool     `json:"needPrompt"`
	Resumed       bool     `json:"resumed"` // the pending prompt is a welcome-back, not a hello
	// Failure state is deliberately not persisted: restarting the engine is a
	// person saying "try again", and it should not inherit an old verdict.
	LaunchRetryAt time.Time `json:"-"`
	Fails         int       `json:"-"`
	Broken        bool      `json:"-"`
	LastErr       string    `json:"-"`
	PaneHash      string    `json:"-"`
	Idle          int       `json:"-"`
}

type runState struct {
	LastReady  string    `json:"lastReady,omitempty"`
	Harness    string    `json:"harness,omitempty"`
	Session    string    `json:"session"`
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
	ObservedAt   time.Time                  `json:"observedAt,omitempty"`
	Observations map[string]RoleObservation `json:"observations,omitempty"`
	Roles        map[string]*roleState      `json:"roles"`
	Runs         map[string]*runState       `json:"runs"`
	Messages     map[string]*msgState       `json:"messages"`
	AuditSeq     int                        `json:"auditSeq"`
}

type Engine struct {
	controls chan controlCall
	root     string
	cfg      config.Config
	st       state
	log      *log.Logger
	logF     *os.File
	lockF    *os.File
	notices  map[string]string
}

func New(root string) (*Engine, error) { return newEngine(root, false) }

// NewControl can stop a company even while its configuration is broken.
func NewControl(root string) (*Engine, error) { return newEngine(root, true) }

func newEngine(root string, control bool) (*Engine, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	cfg, err := config.Load(root)
	if err != nil {
		if !control {
			return nil, err
		}
		cfg = config.Default()
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
	e.recoverSessions()
	return e, nil
}

func (e *Engine) Close() error { e.Unlock(); return e.logF.Close() }

func (e *Engine) statePath() string {
	return filepath.Join(config.LocalDir(e.root), "state.json")
}

func (e *Engine) loadState() {
	b, err := os.ReadFile(e.statePath())
	if err != nil {
		return
	}
	var loaded state
	if err := json.Unmarshal(b, &loaded); err != nil {
		e.log.Printf("cannot read saved state; recovering from tmux: %v", err)
		return
	}
	e.st = loaded
	if e.st.Roles == nil {
		e.st.Roles = map[string]*roleState{}
	}
	if e.st.Runs == nil {
		e.st.Runs = map[string]*runState{}
	}
	if e.st.Messages == nil {
		e.st.Messages = map[string]*msgState{}
	}
	for name, s := range e.st.Roles {
		if s == nil {
			delete(e.st.Roles, name)
		}
	}
	for name, s := range e.st.Runs {
		if s == nil {
			delete(e.st.Runs, name)
		}
	}
	for name, s := range e.st.Messages {
		if s == nil {
			delete(e.st.Messages, name)
		}
	}
}

func (e *Engine) saveState() {
	e.st.ObservedAt = time.Now()
	e.st.Observations = map[string]RoleObservation{}
	for name, s := range e.st.Roles {
		e.st.Observations[name] = RoleObservation{Idle: s.Idle, Broken: s.Broken, Error: s.LastErr}
	}
	if b, err := json.MarshalIndent(e.st, "", "  "); err == nil {
		if err := space.WriteFile(e.statePath(), b, 0o644); err != nil {
			e.log.Printf("cannot save state: %v", err)
		}
	}
}

// reload picks up edits to vcomp.conf between ticks. A broken file keeps the
// last good configuration rather than stopping the company.
func (e *Engine) reload() {
	cfg, err := config.Load(e.root)
	if err != nil {
		e.notice("config", fmt.Sprintf("config not reloaded: %v", err))
		return
	}
	if e.notices["config"] != "" {
		e.notice("config", "configuration reloaded")
		delete(e.notices, "config")
	}
	before, _ := json.Marshal(e.cfg)
	after, _ := json.Marshal(cfg)
	if string(before) != string(after) {
		e.log.Print("configuration changed; new settings loaded")
	}
	e.cfg = cfg
}

// Run ticks until the goal is declared reached or stop is closed. It reports
// whether the company finished.
func (e *Engine) Run(stop <-chan struct{}) bool {
	e.log.Printf("engine started: root=%s tick=%s harness=%s", e.root, e.cfg.Tick, e.cfg.Harness)
	select {
	case <-stop:
		return false
	default:
	}
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
		case call := <-e.controls:
			t.Stop()
			call.reply <- e.directSteer(call.request, stop)
		}
	}
}

// close winds the company up once the goal has been declared reached.
func (e *Engine) close() bool {
	n, err := e.Stop()
	e.log.Printf("goal declared reached; closed %d sessions", n)
	if err != nil {
		e.log.Printf("could not close all sessions: %v", err)
	}
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
	if changed, err := bootstrap.SyncGoal(e.root, e.cfg); err != nil {
		e.notice("goal-error", fmt.Sprintf("cannot update CEO goal: %v", err))
	} else if changed {
		e.log.Print("CEO goal updated")
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
func (e *Engine) Session(role string) string {
	if s := e.st.Roles[role]; s != nil && s.Session != "" {
		return s.Session
	}
	return e.cfg.SessionPrefix + "-" + role
}

// Stop kills every session this engine owns.
func (e *Engine) Stop() (int, error) {
	n := 0
	var errs []error
	for _, sess := range tmux.List() {
		if sessionRoot(sess) != e.root {
			continue
		}
		if err := tmux.Kill(sess); err != nil {
			errs = append(errs, fmt.Errorf("cannot stop %s: %w", sess, err))
		} else {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// Orphans lists all vcomp-owned sessions, including those whose engine stopped.
func Orphans() []string {
	var out []string
	for _, s := range tmux.List() {
		if sessionRoot(s) != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// notice reports a changed observation once. Heartbeats do not become events.
func (e *Engine) notice(key, text string) {
	if e.notices == nil {
		e.notices = map[string]string{}
	}
	if e.notices[key] == text {
		return
	}
	e.notices[key] = text
	e.log.Print(text)
}
