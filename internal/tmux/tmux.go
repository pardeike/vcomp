// Package tmux is a thin wrapper over the tmux CLI. It only knows about
// detached sessions running one command each.
package tmux

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Available reports whether tmux is usable on this machine.
func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func run(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Exists reports whether a session with this name is alive.
func Exists(session string) bool {
	err := exec.Command("tmux", "has-session", "-t", "="+session).Run()
	return err == nil
}

// List returns all session names tmux currently knows about.
func List() []string {
	out, err := run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		return nil // no server running
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l != "" {
			names = append(names, l)
		}
	}
	return names
}

// New starts a detached session named session, with cwd dir, running cmd.
// The pane is kept after the command exits, so a harness that dies on startup
// leaves its error message behind to be read instead of vanishing silently.
func New(session, dir string, cmd []string, metadata ...map[string]string) error {
	if len(cmd) == 0 {
		return errors.New("tmux: empty command")
	}
	// Start a quiet placeholder first. Configure the pane before launching the
	// agent, so even an immediate startup failure leaves a readable dead pane.
	out, err := run("new-session", "-d", "-s", session, "-c", dir, "-P", "-F", "#{pane_id}", "--", "/bin/cat")
	if err != nil {
		return err
	}
	pane := strings.TrimSpace(out)
	fail := func(err error) error { _ = Kill(session); return err }
	if _, err = run("set-option", "-w", "-t", pane, "remain-on-exit", "on"); err != nil {
		return fail(err)
	}
	if err = SetOption(session, "@vcomp-pane", pane); err != nil {
		return fail(err)
	}
	for _, values := range metadata {
		for k, v := range values {
			if err = SetOption(session, k, v); err != nil {
				return fail(err)
			}
		}
	}
	_, err = run(append([]string{"respawn-pane", "-k", "-t", pane, "-c", dir, "--"}, cmd...)...)
	if err != nil {
		return fail(err)
	}
	return nil
}

func SetOption(session, key, value string) error {
	if !Exists(session) {
		return fmt.Errorf("no session %s", session)
	}
	_, err := run("set-option", "-t", session, key, value)
	return err
}

func Option(session, key string) string {
	if session == "" || !Exists(session) {
		return ""
	}
	out, _ := run("show-options", "-qv", "-t", session, key)
	return strings.TrimSpace(out)
}

// Pane is the original agent pane, even if an attached user changes windows
// or splits the session. A missing original pane counts as exited.
type Pane struct {
	Exists, Dead    bool
	ID, Status, TTY string
}

func Inspect(session string) (Pane, error) {
	out, err := run("list-panes", "-s", "-t", "="+session, "-F", "#{pane_id}|#{pane_dead}|#{pane_tty}|#{pane_dead_status}")
	if err != nil {
		if !Exists(session) {
			return Pane{}, nil
		}
		return Pane{}, err
	}
	wanted := Option(session, "@vcomp-pane")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "|")
		if len(fields) < 2 {
			continue
		}
		if wanted != "" && fields[0] != wanted {
			continue
		}
		p := Pane{Exists: true, ID: fields[0], Dead: fields[1] == "1"}
		if len(fields) > 2 {
			p.TTY = fields[2]
		}
		if len(fields) > 3 {
			p.Status = fields[3]
		}
		return p, nil
	}
	return Pane{Exists: true, Dead: true}, nil
}

func target(session string) (string, error) {
	p, err := Inspect(session)
	if err != nil {
		return "", err
	}
	if !p.Exists || p.ID == "" {
		return "", fmt.Errorf("agent pane missing in %s", session)
	}
	return p.ID, nil
}

// Dead reports whether the session's command has exited, leaving the pane
// behind with whatever it printed on its way out.
func Dead(session string) bool { p, err := Inspect(session); return err == nil && p.Exists && p.Dead }

func DeadStatus(session string) string {
	p, err := Inspect(session)
	if err != nil {
		return "?"
	}
	return p.Status
}

func Alive(session string) bool { p, err := Inspect(session); return err == nil && p.Exists && !p.Dead }

// Kill removes a session; killing a missing session is not an error.
func Kill(session string) error {
	if !Exists(session) {
		return nil
	}
	_, err := run("kill-session", "-t", "="+session)
	return err
}

// Capture returns the visible text of the session's active pane.
func Capture(session string) (string, error) {
	pane, err := target(session)
	if err != nil {
		return "", err
	}
	return run("capture-pane", "-p", "-t", pane)
}

// SendKeys sends named tmux keys (Enter, Escape, C-c ...) to the pane.
func SendKeys(session string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	pane, err := target(session)
	if err != nil {
		return err
	}
	_, err = run(append([]string{"send-keys", "-t", pane}, keys...)...)
	return err
}

// SendLine types one line of text into the pane and presses Enter.
// The text must be single-line: a newline would submit it early.
func SendLine(session, text string) error {
	pane, err := target(session)
	if err != nil {
		return err
	}
	text = strings.ReplaceAll(text, "\n", " ")
	if _, err := run("send-keys", "-t", pane, "-l", "--", text); err != nil {
		return err
	}
	// TUIs debounce input; give the harness a beat before committing.
	time.Sleep(300 * time.Millisecond)
	_, err = run("send-keys", "-t", pane, "Enter")
	return err
}

// StartDir remains stable even after the agent changes its working directory.
func StartDir(session string) string {
	pane, err := target(session)
	if err != nil {
		return ""
	}
	out, _ := run("display-message", "-p", "-t", pane, "#{pane_start_path}")
	return strings.TrimSpace(out)
}
