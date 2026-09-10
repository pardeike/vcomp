package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

// RoleObservation is display-only telemetry. Supervision never reads it back.
type RoleObservation struct {
	Idle   int    `json:"idle"`
	Broken bool   `json:"broken,omitempty"`
	Error  string `json:"error,omitempty"`
}

type AgentView struct {
	Name, Session, State, Harness, Model, Output, Error string
	Inbox, Idle                                         int
	Changed                                             time.Time
}

type RunView struct{ Name, State, Session string }

type Observation struct {
	Root                         string
	Config                       config.Config
	Exists, Supervised, Finished bool
	ObservedAt                   time.Time
	Agents                       []AgentView
	Runs                         []RunView
	Error                        string
}

// Supervising checks the kernel lock without creating files or trusting a stale
// PID. Maintenance also owns this lock; the UI labels it "running / updating".
func Supervising(root string) bool {
	f, err := os.Open(filepath.Join(config.LocalDir(root), "engine.pid"))
	if err != nil {
		return false
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false
	}
	return errors.Is(err, syscall.EWOULDBLOCK)
}

// Observe is read-only, including for an empty directory or damaged config.
// It must not construct an Engine: doing so creates bookkeeping and recovers
// ownership. tmux is the live source; saved counters are timestamped telemetry.
func Observe(root string) Observation {
	if p, err := filepath.EvalSymlinks(root); err == nil {
		root = p
	}
	v := Observation{Root: root}
	if cfg, err := config.Load(root); err != nil {
		v.Config = config.Default()
		v.Error = err.Error()
	} else {
		v.Config = cfg
	}
	v.Supervised = Supervising(root)
	var saved state
	if b, err := os.ReadFile(filepath.Join(config.LocalDir(root), "state.json")); err == nil {
		if err = json.Unmarshal(b, &saved); err != nil {
			v.Error = "Saved engine state is unreadable: " + err.Error()
		}
	}
	v.ObservedAt = saved.ObservedAt
	roles, err := space.Roles(root)
	if err != nil && !os.IsNotExist(err) {
		v.Error = err.Error()
	}
	v.Exists = len(roles) > 0
	if v.Config.ResultFile != "" {
		_, err = os.Stat(filepath.Join(root, v.Config.ResultFile))
		v.Finished = err == nil
	}
	owned := map[string]string{}
	for _, sess := range tmux.List() {
		if sessionRoot(sess) == root {
			owned[sess] = tmux.Option(sess, "@vcomp-name")
		}
	}
	for _, r := range roles {
		a := AgentView{Name: r.Name, State: "stopped", Harness: v.Config.HarnessFor(r.Name), Inbox: countDir(r.Inbox()), Changed: newestUnder(r.Dir)}
		a.Model = v.Config.Roles[r.Name].Model
		if a.Model == "" {
			a.Model = v.Config.Harnesses[a.Harness].Model
		}
		sess := r.Session(v.Config.SessionPrefix)
		if s := saved.Roles[r.Name]; s != nil && s.Session != "" {
			sess = s.Session
		}
		for candidate, name := range owned {
			if name == r.Name {
				sess = candidate
				break
			}
		}
		if _, ok := owned[sess]; ok {
			a.Session = sess
			pane, err := tmux.Inspect(sess)
			switch {
			case err != nil:
				a.State = "unknown"
				a.Error = err.Error()
			case pane.Dead:
				a.State = "exited"
			case pane.Exists:
				a.State = "live"
			}
			a.Output, _ = tmux.Capture(sess)
			if s := saved.Roles[r.Name]; s != nil {
				if s.Harness != "" {
					a.Harness = s.Harness
				}
				if a.State == "live" && (s.NeedPrompt || s.NeedShake) {
					a.State = "starting"
				}
			}
		}
		if o, ok := saved.Observations[r.Name]; ok && v.Supervised {
			a.Idle = o.Idle
			a.Error = o.Error
			if o.Broken {
				a.State = "broken"
			} else if o.Error != "" {
				a.State = "error"
			} else if a.State == "live" && o.Idle > 0 {
				a.State = "quiet"
			}
		}
		v.Agents = append(v.Agents, a)
	}
	for _, r := range space.Runs(root) {
		rv := RunView{Name: r.Name, State: "pending"}
		sess := r.Session(v.Config.SessionPrefix)
		if s := saved.Runs[r.Name]; s != nil && s.Session != "" {
			sess = s.Session
		}
		for candidate, name := range owned {
			if name == r.Name {
				sess = candidate
				break
			}
		}
		if _, ok := owned[sess]; ok {
			rv.Session = sess
		}
		switch {
		case r.Done():
			rv.State = "impressions"
		case r.GivenUp():
			rv.State = "abandoned"
		case rv.Session != "" && tmux.Alive(sess):
			rv.State = "live"
		}
		v.Runs = append(v.Runs, rv)
	}
	v.Error = strings.TrimSpace(v.Error)
	return v
}
