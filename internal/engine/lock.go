package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

func (e *Engine) lockPath() string { return filepath.Join(config.LocalDir(e.root), "engine.pid") }
func (e *Engine) registryPath() string {
	return filepath.Join(config.Home(), "engines", space.Hash([]byte(e.root))+".root")
}

// Lock uses a kernel lock, so simultaneous starts cannot both claim a company.
// The file stays in place: unlinking a locked file would allow a second lock.
func (e *Engine) Lock() error {
	if e.lockF != nil {
		return nil
	}
	f, err := os.OpenFile(e.lockPath(), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("company already has an engine or maintenance operation: %w", err)
	}
	// Older engines used a plain PID file without a kernel lock. Do not take
	// their company while that process is still alive during an upgrade.
	b, _ := os.ReadFile(e.lockPath())
	fields := strings.Fields(string(b))
	if len(fields) == 1 {
		pid, _ := strconv.Atoi(fields[0])
		if pid > 0 && syscall.Kill(pid, 0) == nil {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
			return fmt.Errorf("an older engine may still own this company (pid %d); stop that engine before using this version", pid)
		}
	}
	e.lockF = f
	if err = f.Truncate(0); err == nil {
		_, err = f.WriteAt(fmt.Appendf(nil, "%d\nvcomp-lock\n", os.Getpid()), 0)
	}
	if err == nil {
		err = os.MkdirAll(filepath.Dir(e.registryPath()), 0755)
	}
	if err == nil {
		err = space.WriteFile(e.registryPath(), []byte(e.root), 0644)
	}
	if err != nil {
		e.Unlock()
		return err
	}
	// State may have advanced while this Engine was being constructed.
	e.loadState()
	e.recoverSessions()
	return nil
}

func (e *Engine) Unlock() {
	if e.lockF == nil {
		return
	}
	_ = os.Remove(e.registryPath())
	_ = e.lockF.Truncate(0)
	_ = syscall.Flock(int(e.lockF.Fd()), syscall.LOCK_UN)
	_ = e.lockF.Close()
	e.lockF = nil
}

// StopAndLock waits for the running engine to leave, then holds its lock until
// the caller has finished stopping sessions or resetting the company.
func (e *Engine) StopAndLock() error {
	deadline := time.Now().Add(e.cfg.StopTimeout)
	signalled := 0
	for {
		if err := e.Lock(); err == nil {
			return nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		b, err := os.ReadFile(e.lockPath())
		if err != nil {
			return err
		}
		fields := strings.Fields(string(b))
		pid := 0
		if len(fields) > 0 {
			pid, _ = strconv.Atoi(fields[0])
		}
		if pid > 0 && pid != os.Getpid() && pid != signalled {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				return err
			}
			signalled = pid
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("engine did not stop within %s", e.cfg.StopTimeout)
		}
		time.Sleep(e.cfg.StopPoll)
	}
}

// CompanyRoots includes live engines without sessions and orphaned sessions
// with arbitrary prefixes. Stale registry entries are harmless after a crash.
func CompanyRoots() []string {
	roots := map[string]bool{}
	entries, _ := os.ReadDir(filepath.Join(config.Home(), "engines"))
	for _, entry := range entries {
		if b, err := os.ReadFile(filepath.Join(config.Home(), "engines", entry.Name())); err == nil && len(b) > 0 {
			roots[string(b)] = true
		}
	}
	for _, sess := range tmux.List() {
		if root := sessionRoot(sess); root != "" {
			roots[root] = true
		}
	}
	var out []string
	for root := range roots {
		out = append(out, root)
	}
	return out
}

func (e *Engine) recoverSessions() {
	for _, sess := range tmux.List() {
		if sessionRoot(sess) != e.root {
			continue
		}
		name := tmux.Option(sess, "@vcomp-name")
		if tmux.Option(sess, "@vcomp-kind") == "" {
			dir := tmux.StartDir(sess)
			name = filepath.Base(dir)
			if filepath.Base(filepath.Dir(dir)) == space.SpacesDir {
				s := e.st.Roles[name]
				if s == nil {
					b, _ := os.ReadFile(filepath.Join(dir, space.RoleFile))
					s = &roleState{Started: true, Harness: e.cfg.HarnessFor(name), RoleHash: space.Hash(b)}
				}
				s.Session = sess
				e.st.Roles[name] = s
			} else {
				s := e.st.Runs[name]
				if s == nil {
					s = &runState{Attempts: 1, StartedAt: time.Now()}
				}
				s.Session = sess
				e.st.Runs[name] = s
			}
			continue
		}
		raw := tmux.Option(sess, "@vcomp-state")
		switch tmux.Option(sess, "@vcomp-kind") {
		case "role":
			var s roleState
			if json.Unmarshal([]byte(raw), &s) == nil && name != "" {
				s.Session = sess
				e.st.Roles[name] = &s
			}
		case "run":
			var s runState
			if json.Unmarshal([]byte(raw), &s) == nil && name != "" {
				s.Session = sess
				e.st.Runs[name] = &s
			}
		}
	}
}

func (e *Engine) metadata(kind, name string, st any) map[string]string {
	b, _ := json.Marshal(st)
	return map[string]string{"@vcomp-root": e.root, "@vcomp-kind": kind, "@vcomp-name": name, "@vcomp-state": string(b)}
}

func (e *Engine) remember(sess, kind, name string, st any) {
	if sess == "" {
		return
	}
	if !tmux.Exists(sess) || sessionRoot(sess) != e.root {
		return
	}
	if tmux.Option(sess, "@vcomp-root") == "" {
		for k, v := range e.metadata(kind, name, st) {
			if err := tmux.SetOption(sess, k, v); err != nil {
				e.log.Printf("cannot adopt %s: %v", sess, err)
				return
			}
		}
	}
	b, err := json.Marshal(st)
	if err == nil {
		if err = tmux.SetOption(sess, "@vcomp-state", string(b)); err != nil {
			e.log.Printf("%s: cannot save session state: %v", sess, err)
		}
	}
}

func (e *Engine) killSession(sess string) error {
	if !tmux.Exists(sess) {
		return nil
	}
	if sessionRoot(sess) != e.root {
		return fmt.Errorf("session %s is not owned by this company", sess)
	}
	return tmux.Kill(sess)
}

// sessionRoot recognizes tagged sessions and the older layout. A legacy pane
// must have started in a real company space and have that company's session
// name; a similar prefix alone never establishes ownership.
func sessionRoot(sess string) string {
	if root := tmux.Option(sess, "@vcomp-root"); root != "" {
		return root
	}
	dir := tmux.StartDir(sess)
	if dir == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	root := filepath.Dir(filepath.Dir(dir))
	cfg, err := config.Load(root)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(root, config.DirName)); err != nil {
		return ""
	}
	name := filepath.Base(dir)
	switch filepath.Base(filepath.Dir(dir)) {
	case space.SpacesDir:
		if sess != cfg.SessionPrefix+"-"+name {
			return ""
		}
	case space.PublicDir:
		if !strings.HasPrefix(name, "run-") || sess != cfg.SessionPrefix+"-user-"+name {
			return ""
		}
	default:
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, space.RoleFile)); err != nil {
		return ""
	}
	return root
}
