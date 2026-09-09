// What the company looks like from outside: the state file the roles read and
// the status the operator reads.
package engine

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

// publish writes a short, current picture of the company for anyone who wants
// it. It is deliberately a file rather than something pushed into a prompt:
// information here is pulled, and an overview nobody asked for is just another
// thing filling up a context window.
func (e *Engine) publish(roles []space.Role) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Company state\n\nRewritten by the engine every %s. Read it, do not edit it.\n\n",
		e.cfg.Tick)
	fmt.Fprintf(&b, "%-20s %-9s %6s %6s  %s\n", "ROLE", "SESSION", "INBOX", "IDLE", "SPACE LAST CHANGED")
	for _, r := range roles {
		session := "stopped"
		if tmux.Alive(e.Session(r.Name)) {
			session = "running"
		}
		e.notice("inbox/"+r.Name, fmt.Sprintf("%s: inbox %d", r.Name, countDir(r.Inbox())))
		idle := 0
		if st := e.st.Roles[r.Name]; st != nil {
			if st.Broken {
				session = "broken"
			}
			idle = st.Idle
		}
		fmt.Fprintf(&b, "%-20s %-9s %6d %6d  %s\n",
			r.Name, session, countDir(r.Inbox()), idle, age(newestUnder(r.Dir)))
	}

	product := e.productLine()
	e.notice("product", "product: "+product)
	fmt.Fprintf(&b, "\nPRODUCT  %s\n", product)

	var done, waiting, gaveUp int
	for _, run := range space.Runs(e.root) {
		switch {
		case run.Done():
			done++
		case run.GivenUp():
			gaveUp++
		default:
			waiting++
		}
	}
	fmt.Fprintf(&b, "PUBLIC   %d user runs: %d with impressions, %d in progress, %d abandoned\n",
		done+waiting+gaveUp, done, waiting, gaveUp)

	if e.cfg.StateFile != "" {
		_ = space.WriteFile(filepath.Join(e.root, e.cfg.StateFile), []byte(b.String()), 0o644)
	}
}

func (e *Engine) productLine() string {
	dir := filepath.Join(e.root, space.ProductDir)
	count, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		return "no commits yet"
	}
	last, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%h %s").Output()
	if err != nil {
		return strings.TrimSpace(string(count)) + " commits"
	}
	n := strings.TrimSpace(string(count))
	word := " commits"
	if n == "1" {
		word = " commit"
	}
	return n + word + ", last: " + strings.TrimSpace(string(last))
}

func countDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// newestUnder is the most recent change anywhere in a role's space, which is
// the cheapest honest answer to "is this person doing anything".
func newestUnder(dir string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest
}

func age(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm ago", int(d.Hours()), int(d.Minutes())%60)
	}
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
		if tmux.Alive(e.Session(r.Name)) {
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
		case tmux.Alive(e.runSession(run)):
			status = "user in session"
		}
		fmt.Fprintf(w, "  %-12s %s\n", run.Name, status)
	}
}
