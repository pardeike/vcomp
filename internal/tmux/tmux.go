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
func New(session, dir string, cmd []string) error {
	if len(cmd) == 0 {
		return errors.New("tmux: empty command")
	}
	args := append([]string{"new-session", "-d", "-s", session, "-c", dir, "--"}, cmd...)
	_, err := run(args...)
	return err
}

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
	return run("capture-pane", "-p", "-t", session)
}

// SendLine types one line of text into the pane and presses Enter.
// The text must be single-line: a newline would submit it early.
func SendLine(session, text string) error {
	text = strings.ReplaceAll(text, "\n", " ")
	if _, err := run("send-keys", "-t", session, "-l", "--", text); err != nil {
		return err
	}
	// TUIs debounce input; give the harness a beat before committing.
	time.Sleep(300 * time.Millisecond)
	_, err := run("send-keys", "-t", session, "Enter")
	return err
}
