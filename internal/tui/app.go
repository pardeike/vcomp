package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/engine"
	"vcomp/internal/space"
)

type Options struct {
	Screen                       int
	Start                        bool
	Goal, GoalFile, Action, Name string
}
type refreshResult struct {
	Root string
	Data data
}

type result struct {
	Draft *professionDraft
	Text  string
	Err   error
	Kind  string
}
type app struct {
	generationCancel context.CancelFunc
	m                model
	t                *terminal
	child            *exec.Cmd
	childDone        chan result
	jobs             chan result
	refresh          chan refreshResult
	reading          bool
}

func Run(root string, opt Options) error {
	if !Interactive() {
		return fmt.Errorf("the TUI requires an interactive terminal; use --plain for command-line output")
	}
	t, err := openTerminal()
	if err != nil {
		return err
	}
	a := &app{m: model{Root: root, Screen: opt.Screen, Follow: opt.Screen == 5, Message: "Loading company..."}, t: t, jobs: make(chan result, 1), refresh: make(chan refreshResult, 1)}
	defer func() {
		if a.generationCancel != nil {
			a.generationCancel()
		}
		a.closeChild()
		if a.t != nil {
			a.t.Close()
		}
	}()
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	defer signal.Stop(signals)
	tick := time.NewTicker(config.Default().TUIRefresh)
	defer tick.Stop()
	a.reload()
	initial := true
	for {
		w, h := a.t.Size()
		frame := draw(a.m.render(w, h), os.Getenv("NO_COLOR") == "")
		if a.m.CursorX >= 0 && a.m.CursorY >= 0 {
			frame += fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", a.m.CursorY+1, a.m.CursorX+1)
		} else {
			frame += "\x1b[?25l"
		}
		fmt.Fprint(os.Stdout, frame)
		select {
		case sig := <-signals:
			if sig != syscall.SIGWINCH {
				return nil
			}
		case k := <-a.t.keys:
			act := a.m.key(k)
			if act != nil {
				quit, err := a.dispatch(*act)
				if err != nil {
					if a.t == nil {
						return err
					}
					a.m.Message = err.Error()
				}
				if quit {
					return nil
				}
			}
		case refreshed := <-a.refresh:
			a.reading = false
			if refreshed.Root != a.m.Root {
				a.reload()
				continue
			}
			d := refreshed.Data
			a.m.update(d)
			if d.View.Supervised && a.m.Message == "Starting supervision..." {
				a.m.Message = ""
			}
			if d.View.Config.TUIRefresh > 0 {
				tick.Reset(d.View.Config.TUIRefresh)
			}
			if initial {
				initial = false
				a.m.Message = ""
				switch {
				case opt.Goal != "" || opt.GoalFile != "":
					if !d.View.Exists {
						a.m.Busy = true
						go func() { // Create through the same bootstrap path; start supervision afterwards.
							cfg, err := config.Load(root)
							if err == nil && opt.GoalFile != "" {
								var b []byte
								b, err = os.ReadFile(opt.GoalFile)
								cfg.Goal = string(b)
							}
							if opt.Goal != "" {
								cfg.Goal = opt.Goal
							}
							if err == nil {
								err = bootstrap.Init(root, cfg)
							}
							a.jobs <- result{Kind: "created-start", Err: err}
						}()
					} else if opt.Start {
						_, err = a.dispatch(action{Kind: "start"})
					}
				case opt.Action != "":
					_, err = a.dispatch(action{Kind: opt.Action, Values: []string{opt.Name}})
				case opt.Screen == 3 || !d.View.Exists:
					_, err = a.dispatch(action{Kind: "settings-form"})
				case opt.Start:
					_, err = a.dispatch(action{Kind: "start"})
				}
				if err != nil {
					a.m.Message = err.Error()
				}
			}
		case r := <-a.jobs:
			a.m.Busy = false
			if r.Draft != nil {
				a.m.Draft = r.Draft
				a.m.Form = nil
				a.m.Detail = "profession-draft"
				a.m.Scroll = 0
				a.m.Message = r.Text
				if r.Err != nil {
					a.m.Message = r.Err.Error()
				}
				a.reload()
				continue
			}
			if r.Err != nil {
				a.m.Message = r.Err.Error()
			} else {
				a.m.Form = nil
				a.m.Message = r.Text
				switch r.Kind {
				case "profession-save", "profession-delete", "profession-restore":
					a.m.switchScreen(6)
					a.m.Draft = nil
					a.m.Message = r.Text
				case "save-settings", "hire", "replace", "steer":
					a.m.switchScreen(0)
					a.m.Message = r.Text
				case "test":
					a.m.switchScreen(1)
					a.m.Message = r.Text
				case "created-start":
					_, err = a.dispatch(action{Kind: "start"})
					if err != nil {
						a.m.Message = err.Error()
					}
				}
			}
			a.reload()
		case r := <-a.childDone:
			a.child = nil
			a.childDone = nil
			a.m.OwnEngine = false
			if r.Err != nil {
				a.m.Message = "Engine ended: " + r.Err.Error() + " " + r.Text
			} else {
				a.m.Message = "Supervisor ended. See Goal / result or Activity for details."
			}
			a.reload()
		case <-tick.C:
			a.reload()
		}
	}
}
func (a *app) reload() {
	if a.reading {
		return
	}
	a.reading = true
	root := a.m.Root
	go func() { a.refresh <- refreshResult{Root: root, Data: loadData(root)} }()
}
func (a *app) closeChild() {
	if a.child == nil {
		return
	}
	_ = a.child.Process.Signal(os.Interrupt)
	timeout := a.m.Data.View.Config.StopTimeout
	if timeout <= 0 {
		timeout = config.Default().StopTimeout
	}
	select {
	case <-a.childDone:
	case <-time.After(timeout):
		_ = a.child.Process.Kill()
		<-a.childDone
	}
	a.child = nil
	a.childDone = nil
	a.m.OwnEngine = false
}
func (a *app) work(kind string, fn func() (string, error)) {
	a.m.Busy = true
	go func() { s, err := fn(); a.jobs <- result{Kind: kind, Text: s, Err: err} }()
}
func (a *app) dispatch(act action) (bool, error) {
	root := a.m.Root
	if act.Kind == "sort-form" || act.Kind == "sort-save" {
		return false, a.sortAction(act)
	}
	if strings.HasPrefix(act.Kind, "profession-") || strings.HasPrefix(act.Kind, "generation-") {
		return false, a.professionAction(act)
	}
	switch act.Kind {
	case "quit":
		return true, nil
	case "start":
		if !bootstrap.Exists(root) {
			return a.dispatch(action{Kind: "settings-form"})
		}
		if a.child != nil || engine.Supervising(root) {
			a.m.switchScreen(0)
			a.m.Message = "Showing the existing engine."
			return false, nil
		}
		exe, err := os.Executable()
		if err != nil {
			return false, err
		}
		c := exec.Command(exe, "run", "-root", root, "--plain")
		var stderr bytes.Buffer
		c.Stdout = io.Discard
		c.Stderr = &stderr
		if err = c.Start(); err != nil {
			return false, err
		}
		a.child = c
		a.childDone = make(chan result, 1)
		done := a.childDone
		go func() { err := c.Wait(); done <- result{Err: err, Text: stderr.String()} }()
		a.m.OwnEngine = true
		a.m.switchScreen(0)
		a.m.Message = "Starting supervision..."
		a.reload()
	case "settings-form":
		f, err := settingsForm(root)
		if err != nil {
			return false, fmt.Errorf("%v; press e on Settings to repair the file", err)
		}
		a.m.Form = f
	case "save-settings":
		a.work(act.Kind, func() (string, error) {
			err := saveSettings(root, act.Values)
			return "Company settings saved. Press s to start supervision.", err
		})
	case "role-settings-form":
		f, err := roleSettingsForm(root, act.Values[0])
		if err != nil {
			return false, err
		}
		a.m.Form = f
	case "role-settings":
		a.work(act.Kind, func() (string, error) {
			return "Employee settings saved; running conversations continue until their next restart.", saveRoleSettings(root, act.Values)
		})
	case "hire-form":
		position := "developer"
		if a.m.Screen == 6 {
			p := a.m.profession()
			if p.Name == "" {
				return false, fmt.Errorf("select a profession")
			}
			if p.Deleted {
				return false, fmt.Errorf("restore this profession before hiring")
			}
			position = p.Name
		}
		name := ""
		if len(act.Values) > 0 {
			name = act.Values[0]
		}
		a.m.Form = hireForm(a.m.Data.Positions, a.m.Data.View.Agents, position, name)
	case "replace-form":
		name := act.Values[0]
		if name == "ceo" {
			return false, fmt.Errorf("the CEO's identity is user-owned; edit its template or instructions in Settings")
		}
		a.m.Form = newForm("replace", "Replace "+name, []field{{Label: "Employee", Value: name, Kind: kindStatic}, {Label: "New backstory", Empty: "optional; drawn from the pool"}}, nil)
	case "message-form":
		name := act.Values[0]
		if name == "" {
			name = a.m.agent()
		}
		if name == "" {
			return false, fmt.Errorf("select an employee")
		}
		a.m.Form = newForm("message", "Send message FROM USER to "+name, []field{{Label: "Recipient", Value: name, Kind: kindStatic}, {Label: "Subject"}, {Label: "Message"}, {Label: "Or read from file", Kind: kindFile, Base: root, Empty: "none"}}, nil)
	case "message":
		a.work(act.Kind, func() (string, error) {
			args := []string{"message", act.Values[0], "-subject", act.Values[1], "-text", act.Values[2]}
			if act.Values[3] != "" {
				args = append(args, "-file", absolute(act.Values[3], root))
			}
			return command(root, args...)
		})
	case "direct-steer-form", "broadcast-form":
		name := a.m.agent()
		if len(act.Values) > 0 && act.Values[0] != "" {
			name = act.Values[0]
		}
		title := "Direct steer to " + name
		if act.Kind == "broadcast-form" {
			name = "All running employees"
			title = "Broadcast to all running employees (including CEO; excluding public testers)"
		}
		if name == "" {
			return false, fmt.Errorf("select an employee")
		}
		a.m.Form = newForm("direct-steer", title, []field{
			{Label: "Recipient", Value: name, Kind: kindStatic},
			{Label: "Delivery", Value: "queued", Kind: kindChoice, Options: []option{{Value: "queued", Title: "Queued: after current work finishes"}, {Value: "immediate", Title: "Immediate: interrupt, then submit in the same conversation"}}},
			{Label: "Prompt"}, {Label: "Or read from file", Kind: kindFile, Base: root, Empty: "none"},
		}, nil)
	case "direct-steer":
		a.work(act.Kind, func() (string, error) {
			args := []string{"direct-steer", "-mode", act.Values[1], "-text", act.Values[2]}
			if act.Values[0] == "All running employees" {
				args = append(args, "-all")
			} else {
				args = append(args, act.Values[0])
			}
			if act.Values[3] != "" {
				args = append(args, "-file", absolute(act.Values[3], root))
			}
			return command(root, args...)
		})
	case "steer-form":
		name := act.Values[0]
		if name == "ceo" {
			return false, fmt.Errorf("use CEO instructions file in Settings for the CEO")
		}
		a.m.Form = newForm("steer", "Steer "+name, []field{{Label: "Employee", Value: name, Kind: kindStatic}, {Label: "Steering text", Empty: "empty clears the current steering"}, {Label: "Or read from file", Kind: kindFile, Base: root, Empty: "none"}}, nil)
	case "hire":
		a.work(act.Kind, func() (string, error) {
			return command(root, "hire", act.Values[0], "-position", act.Values[1], "-backstory", act.Values[2])
		})
	case "replace":
		if len(act.Values) < 2 {
			return false, fmt.Errorf("incomplete replacement")
		}
		a.m.Confirm = &action{Kind: "replace-confirmed", Values: act.Values}
		a.m.Confirmation = "Replace " + act.Values[0] + "? Their profession, steering, notes, and files survive; their conversation starts fresh."
	case "replace-confirmed":
		a.work("replace", func() (string, error) {
			return command(root, "hire", act.Values[0], "-backstory", act.Values[1], "-replace")
		})
	case "steer":
		a.work(act.Kind, func() (string, error) {
			args := []string{"steer", act.Values[0], "-text", act.Values[1]}
			if act.Values[2] != "" {
				args = append(args, "-file", absolute(act.Values[2], root))
			}
			return command(root, args...)
		})
	case "test-form":
		a.m.Form = newForm("test", "Create a public user test", []field{{Label: "Instructions", Empty: "optional; the role template applies"}, {Label: "Or instructions file", Kind: kindFile, Base: root, Empty: "none"}}, nil)
	case "test":
		a.work(act.Kind, func() (string, error) {
			if !bootstrap.Exists(root) {
				return "", fmt.Errorf("set up the company first")
			}
			args := []string{"user-run", "-text", act.Values[0]}
			if act.Values[1] != "" {
				args = append(args, "-instructions", absolute(act.Values[1], root))
			}
			return command(root, args...)
		})
	case "stop-confirm":
		a.m.Confirm = &action{Kind: "stop"}
		a.m.Confirmation = "Stop this company's engine and agent sessions? Files and history are kept."
	case "stop":
		a.work(act.Kind, func() (string, error) { return command(root, "stop") })
	case "reset-confirm":
		if !bootstrap.Exists(root) {
			return false, fmt.Errorf("no company here to reset")
		}
		cfg, err := config.Load(root)
		if err != nil {
			return false, err
		}
		paths := bootstrap.Produced(root, cfg)
		for i, p := range paths {
			paths[i] = strings.TrimPrefix(p, root+string(filepath.Separator))
		}
		a.m.Confirm = &action{Kind: "reset"}
		a.m.Confirmation = "Reset " + root + "?\nDeletes: " + strings.Join(paths, ", ") + "\nKeeps company settings and template overrides. Stops its engine and agents."
	case "reset":
		a.work(act.Kind, func() (string, error) { return command(root, "reset", "-y") })
	case "open":
		if a.child != nil {
			return false, fmt.Errorf("stop this run with x before opening another company")
		}
		path, err := filepath.Abs(act.Values[0])
		if err != nil {
			return false, err
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return false, fmt.Errorf("choose an existing directory")
		}
		a.m.Root = path
		a.m.Form = nil
		a.m.Data = data{}
		a.m.switchScreen(0)
		a.reload()
	case "editor":
		return false, a.edit()
	case "terminal-verbosity":
		mode := "brief"
		switch a.m.Data.View.Config.TerminalView {
		case "brief", "":
			mode = "detailed"
		case "detailed":
			mode = "raw"
		}
		if err := config.UpdateLocal(root, []config.Override{{Key: "terminal_view", Value: mode}}); err != nil {
			return false, err
		}
		a.m.Data.View.Config.TerminalView = mode
		a.m.TerminalSnapshot = ""
		a.m.Follow = true
		a.m.Scroll = 0
		a.reload()
	case "intervene-confirm":
		if len(act.Values) == 0 || act.Values[0] == "" {
			return false, fmt.Errorf("this employee has no session")
		}
		a.m.Confirm = &action{Kind: "attach", Values: act.Values}
		back := "Ctrl-B, then d"
		if os.Getenv("TMUX") != "" {
			back = "Ctrl-B, then L"
		}
		a.m.Confirmation = "Enter interactive intervention? Keys go directly to the agent; Escape may interrupt it. Return to vcomp with " + back + "."
	case "attach":
		if len(act.Values) == 0 || act.Values[0] == "" {
			return false, fmt.Errorf("this employee has no session")
		}
		if os.Getenv("TMUX") != "" {
			c := exec.Command("tmux", "switch-client", "-t", act.Values[0])
			b, err := c.CombinedOutput()
			if err != nil {
				return false, fmt.Errorf("cannot switch tmux client: %s", strings.TrimSpace(string(b)))
			}
			a.m.Message = "Interactive agent session. Ctrl-B then L returns to the dashboard."
			return false, nil
		}
		return false, a.external(exec.Command("tmux", "attach-session", "-t", act.Values[0]))
	}
	return false, nil
}
func (a *app) external(c *exec.Cmd) error {
	a.t.Close()
	a.t = nil
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := c.Run()
	t, termErr := openTerminal()
	if termErr != nil {
		return fmt.Errorf("cannot restore TUI: %w", termErr)
	}
	a.t = t
	a.reload()
	return err
}
func (a *app) edit() error {
	root := a.m.Root
	path := filepath.Join(config.LocalDir(root), config.FileName)
	if a.m.Detail == "agent" {
		files := []string{"", "notes.md", "", "goals.md", ""}
		name := files[a.m.Sub]
		if name == "" {
			return fmt.Errorf("this view is read-only; use steering to change role instructions")
		}
		path = filepath.Join(root, space.SpacesDir, a.m.agent(), name)
	} else if a.m.Screen == 4 {
		path = filepath.Join(root, space.SpacesDir, "ceo", "goal.md")
		if a.m.Data.View.Config.GoalFile != "" {
			path = a.m.Data.View.Config.GoalFile
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
		} else {
			return fmt.Errorf("set a Goal file in Settings, then edit it here; generated goal.md is replaced by the engine")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	args := strings.Fields(editor)
	if len(args) == 0 {
		return fmt.Errorf("EDITOR is empty")
	}
	return a.external(exec.Command(args[0], append(args[1:], path)...))
}
