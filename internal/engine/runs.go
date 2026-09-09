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
