// Keeping the employees running: hiring them, noticing when they die,
// replacing them when their role.md is rewritten, and prodding the stuck.
package engine

import (
	"os"
	"strings"

	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

func (e *Engine) syncRoles(roles []space.Role) {
	present := map[string]bool{}
	for _, r := range roles {
		present[r.Name] = true
		e.syncRole(r)
	}
	for name := range e.st.Roles {
		if !present[name] {
			_ = tmux.Kill(e.cfg.SessionPrefix + "-" + name)
			delete(e.st.Roles, name)
			e.log.Printf("%s: space gone, role removed", name)
		}
	}
}

func (e *Engine) syncRole(r space.Role) {
	s := e.st.Roles[r.Name]
	if s == nil {
		s = &roleState{}
		e.st.Roles[r.Name] = s
	}
	sess := r.Session(e.cfg.SessionPrefix)

	// An unusable configuration is reported once and then waited out, since
	// fixing it has to be enough to bring the role back.
	start, err := e.cfg.CommandFor(r.Name, false)
	if err != nil {
		if s.LastErr != err.Error() {
			s.LastErr = err.Error()
			e.log.Printf("%s: %v", r.Name, err)
		}
		return
	}
	s.LastErr = ""
	cmd := strings.Join(start, " ")

	// Rewriting role.md is the only thing that ends a living session: the CEO
	// replacing the occupant. Everything else - a new model, a new effort, a
	// different harness - waits until the session is gone anyway, because the
	// history in a running session is the whole point of keeping it running.
	if s.RoleHash != "" && s.RoleHash != r.Hash {
		e.log.Printf("%s: role.md rewritten - occupant replaced", r.Name)
		_ = tmux.Kill(sess)
		*s = roleState{}
	}
	s.RoleHash = r.Hash

	harness := e.cfg.HarnessFor(r.Name)
	if s.Harness != "" && s.Harness != harness {
		s.Started = false // there is no conversation to resume in a new harness
	}
	if s.Cmd != "" && s.Cmd != cmd {
		s.Fails, s.Broken = 0, false // the command changed, so it deserves another go
	}
	s.Harness, s.Cmd = harness, cmd

	// A pane whose command exited is kept, so we can say why it died instead of
	// restarting it forever in silence.
	if tmux.Exists(sess) && tmux.Dead(sess) {
		e.roleDied(r.Name, s, sess)
	}
	if s.Broken {
		return
	}

	if !tmux.Exists(sess) {
		e.hire(r, s)
		return
	}
	// Surviving a whole tick is what counts as working.
	s.Fails = 0

	// Answer whatever the harness asks before it will listen, one tick before
	// the prompt, so the dialog has settled.
	if s.NeedShake {
		_ = tmux.SendKeys(sess, e.cfg.Handshake(r.Name))
		s.NeedShake = false
		return
	}

	if s.NeedPrompt {
		kind := config.PromptFresh
		if s.Resumed {
			kind = config.PromptBack
		}
		e.send(sess, e.cfg.Prompt(r.Name, kind))
		s.NeedPrompt, s.Idle, s.PaneHash = false, 0, ""
		return
	}

	// A pane whose text has not changed at all for several ticks is stuck: a
	// working agent always animates something.
	pane, err := tmux.Capture(sess)
	if err != nil {
		return
	}
	if h := space.Hash([]byte(pane)); h == s.PaneHash {
		s.Idle++
	} else {
		s.PaneHash, s.Idle = h, 0
	}
	// Someone with nothing in their inbox is left alone for longer.
	empty := inboxEmpty(r)
	if threshold := e.cfg.IdleThreshold(r.Name, empty); s.Idle >= threshold {
		// Not logged: nudging is the engine's normal heartbeat, not an event.
		e.send(sess, e.cfg.Prompt(r.Name, config.PromptNudge))
		s.Idle, s.PaneHash = 0, ""
	}
}

// hire starts a role's session, resuming its previous conversation if it has one.
func (e *Engine) hire(r space.Role, s *roleState) {
	resume := s.Started
	cmd, err := e.cfg.CommandFor(r.Name, resume)
	if err != nil {
		e.log.Printf("%s: %v", r.Name, err)
		return
	}
	if err := tmux.New(r.Session(e.cfg.SessionPrefix), r.Dir, cmd); err != nil {
		e.log.Printf("%s: cannot start session: %v", r.Name, err)
		return
	}
	verb := "hired"
	if resume {
		verb = "revived"
	}
	e.log.Printf("%s: %s", r.Name, verb)
	s.Started, s.NeedPrompt, s.Resumed = true, true, resume
	s.NeedShake = len(e.cfg.Handshake(r.Name)) > 0
	s.Idle, s.PaneHash = 0, ""
}

// roleDied handles a harness that exited.
func (e *Engine) roleDied(name string, s *roleState, sess string) {
	out, _ := tmux.Capture(sess)
	status := tmux.DeadStatus(sess)
	_ = tmux.Kill(sess)
	s.NeedShake, s.NeedPrompt = false, false

	// A resume that dies means there was no conversation to come back to, not
	// that the harness is broken. Fall back to a fresh start, and do not hold
	// it against the role - otherwise a company whose roles have never run
	// under this harness condemns every one of them on the first tick.
	if s.Resumed {
		s.Started, s.Resumed = false, false
		e.log.Printf("%s: nothing to resume, starting fresh", name)
		return
	}

	s.Fails++
	switch {
	case s.Fails >= e.cfg.MaxRestarts:
		s.Broken = true
		e.log.Printf("%s: harness exited %d times (last status %s), giving up until role.md or the config changes\n"+
			"    command: %s\n%s", name, s.Fails, status, s.Cmd, indent(lastLines(out, 8)))
	case s.Fails == 1:
		e.log.Printf("%s: session exited, restarting", name)
	}
}

// lastLines returns the tail of a dead pane: the part that says what went wrong.
func lastLines(out string, n int) []string {
	var kept []string
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimRight(l, " "))
		}
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return kept
}

func indent(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString("    | " + l + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// inboxEmpty reports whether anyone has asked this role for anything.
func inboxEmpty(r space.Role) bool {
	entries, err := os.ReadDir(r.Inbox())
	return err != nil || len(entries) == 0
}

func (e *Engine) send(session, text string) {
	if text == "" {
		return
	}
	if err := tmux.SendLine(session, text); err != nil {
		e.log.Printf("%s: cannot send prompt: %v", session, err)
	}
}
