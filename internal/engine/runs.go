// Public user runs: a throwaway agent, a snapshot of the product, and an
// impressions file that ends the run.
package engine

import (
	"fmt"
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

func (e *Engine) runSession(run space.Run) string {
	if s := e.st.Runs[run.Name]; s != nil && s.Session != "" {
		return s.Session
	}
	return run.Session(e.cfg.SessionPrefix)
}

func (e *Engine) syncRuns() {
	present := map[string]bool{}
	defer func() {
		for name, s := range e.st.Runs {
			if !present[name] {
				if s != nil && s.Session != "" {
					if err := e.killSession(s.Session); err != nil {
						continue
					}
				}
				delete(e.st.Runs, name)
			} else if s != nil {
				e.remember(s.Session, "run", name, s)
			}
		}
	}()
	for _, run := range space.Runs(e.root) {
		present[run.Name] = true
		rs := e.st.Runs[run.Name]
		if rs == nil {
			rs = &runState{}
			e.st.Runs[run.Name] = rs
		}
		sess := e.runSession(run)
		if tmux.Exists(sess) && sessionRoot(sess) != e.root {
			e.notice("run-error/"+run.Name, fmt.Sprintf("%s: session name belongs to another company", run.Name))
			continue
		}
		if tmux.Exists(sess) && tmux.Dead(sess) {
			if err := e.killSession(sess); err != nil {
				continue
			}
			rs.Session = "" // keep attempts across exits
		}
		alive := tmux.Alive(sess)

		if run.Done() {
			if alive {
				_ = e.killSession(e.runSession(run))
				e.log.Printf("%s: user finished, impressions.md written", run.Name)
			}
			continue
		}
		if run.GivenUp() {
			if alive {
				_ = e.killSession(e.runSession(run))
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
			e.notice("run-error/"+run.Name, fmt.Sprintf("%s: cannot prepare: %v", run.Name, err))
			continue
		}
		// Users never resume: every run is someone who has never seen this before.
		// The empty role selects [user] overrides, including its harness handshake.
		cmd, err := e.cfg.CommandFor("", false)
		if err != nil {
			e.notice("run-error/"+run.Name, fmt.Sprintf("%s: %v", run.Name, err))
			continue
		}
		next := *rs
		next.Harness = e.cfg.HarnessFor("")
		next.Attempts++
		next.StartedAt = time.Now()
		next.Session = run.Session(e.cfg.SessionPrefix)
		next.NeedPrompt, next.Idle, next.PaneHash, next.LastReady = true, 0, "", ""
		next.NeedShake = len(e.cfg.Handshake("")) > 0
		if err := tmux.New(next.Session, run.Dir, cmd, e.metadata("run", run.Name, &next)); err != nil {
			e.notice("run-error/"+run.Name, fmt.Sprintf("%s: cannot start user: %v", run.Name, err))
			rs.Attempts++
			continue
		}
		delete(e.notices, "run-error/"+run.Name)
		*rs = next
		e.log.Printf("%s: user run started (attempt %d)", run.Name, rs.Attempts)
	}
}

// pokeUser drives a live user session: prompt it, nudge it if it stalls, and
// kill it if it overruns.
func (e *Engine) pokeUser(run space.Run, rs *runState) {
	if time.Since(rs.StartedAt) > e.cfg.UserTimeout {
		_ = e.killSession(e.runSession(run))
		e.log.Printf("%s: user run timed out", run.Name)
		return
	}
	if rs.NeedShake {
		if err := tmux.SendKeys(e.runSession(run), e.cfg.Handshake("")); err != nil {
			return
		}
		rs.NeedShake = false
		return
	}
	if rs.NeedPrompt {
		if !e.promptReady(e.runSession(run), e.runHarness(rs)) {
			return
		}
		if !e.sendWhenReady(e.runSession(run), e.runHarness(rs), e.cfg.Prompt("", config.PromptUser), &rs.LastReady) {
			return
		}
		rs.NeedPrompt, rs.Idle, rs.PaneHash = false, 0, ""
		return
	}
	pane, err := tmux.Capture(e.runSession(run))
	if err != nil {
		return
	}
	if h := space.Hash([]byte(pane)); h == rs.PaneHash {
		rs.Idle++
	} else {
		rs.PaneHash, rs.Idle = h, 0
	}
	if !e.promptReady(e.runSession(run), e.runHarness(rs)) {
		rs.Idle = 0
		return
	}
	if rs.Idle >= e.cfg.IdleThreshold("", false) {
		if e.sendWhenReady(e.runSession(run), e.runHarness(rs), e.cfg.Prompt("", config.PromptUserNudge), &rs.LastReady) {
			rs.Idle, rs.PaneHash = 0, ""
		}
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
	tmp, err := os.MkdirTemp(run.Dir, ".snapshot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := copyTree(src, tmp); err != nil {
		return err
	}
	version := "unknown"
	if out, err := exec.Command("git", "-C", src, "log", "-1", "--format=%h %s").Output(); err == nil {
		version = strings.TrimSpace(string(out))
	}
	if err := space.WriteFile(filepath.Join(run.Dir, "version.txt"), []byte(version+"\n"), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
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
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(target, filepath.Join(dst, rel))
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

func (e *Engine) runHarness(rs *runState) string {
	if rs.Harness != "" {
		return rs.Harness
	}
	return e.cfg.HarnessFor("")
}
