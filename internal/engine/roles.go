// Keeping the employees running: hiring them, noticing when they die,
// replacing them when their role.md is rewritten, and prodding the stuck.
package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"vcomp/internal/bootstrap"

	"vcomp/internal/config"
	"vcomp/internal/harness"
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
			if err := e.killSession(e.Session(name)); err != nil {
				e.log.Printf("%s: cannot remove session: %v", name, err)
				continue
			}
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
	defer func() { e.remember(s.Session, "role", r.Name, s) }()
	refreshed, err := bootstrap.RefreshRole(e.root, e.cfg, r.Name)
	if err != nil {
		e.roleError(r.Name, s, err)
		return
	}
	r = refreshed
	sess := e.Session(r.Name)
	pane, err := tmux.Inspect(sess)
	if err != nil {
		e.roleError(r.Name, s, err)
		return
	}
	if pane.Exists && sessionRoot(sess) != e.root {
		e.roleError(r.Name, s, fmt.Errorf("session %s belongs to another company or was not created by vcomp; choose another session_prefix", sess))
		return
	}
	if s.RoleHash != "" && s.RoleHash != r.Hash {
		if err := e.killSession(sess); err != nil {
			e.roleError(r.Name, s, err)
			return
		}
		e.log.Printf("%s: role inputs changed - occupant replaced", r.Name)
		*s = roleState{}
		pane = tmux.Pane{}
	}
	s.RoleHash = r.Hash
	// Any relevant configuration edit permits a broken role to try again. Keep
	// the actual running harness separate from the settings for its next start.
	settings, _ := json.Marshal(struct {
		Role     config.Role
		Harness  config.Harness
		Name     string
		Restarts int
	}{
		e.cfg.Roles[r.Name], e.cfg.Harnesses[e.cfg.HarnessFor(r.Name)], e.cfg.HarnessFor(r.Name), e.cfg.MaxRestarts})
	hash := space.Hash(settings)
	if hash != s.SettingsHash {
		s.Fails, s.Broken = 0, false
		s.LaunchRetryAt = time.Time{}
		s.SettingsHash = hash
	}
	if pane.Dead || !pane.Exists && s.Session != "" {
		if !e.roleDied(r.Name, s, sess) {
			return
		}
		pane = tmux.Pane{}
	}
	if s.Broken {
		return
	}
	if !pane.Exists {
		if time.Now().Before(s.LaunchRetryAt) {
			return
		}
		e.hire(r, s)
		return
	}
	if s.DirectError != "" {
		s.LastErr = s.DirectError
		return
	}
	if s.NeedShake {
		if err := tmux.SendKeys(sess, e.cfg.Harnesses[s.Harness].Handshake); err != nil {
			e.roleError(r.Name, s, err)
			return
		}
		e.log.Printf("%s: startup handshake sent", r.Name)
		s.LastErr = ""
		s.NeedShake = false
		return
	}
	if s.NeedPrompt {
		if !e.promptReady(sess, s.Harness) {
			return
		}
		kind := config.PromptFresh
		if s.Resumed {
			kind = config.PromptBack
		}
		if !e.sendWhenReady(sess, s.Harness, e.cfg.Prompt(r.Name, kind), &s.LastReady) {
			return
		}
		e.log.Printf("%s: %s prompt sent", r.Name, kind)
		s.LastErr = ""
		s.NeedPrompt, s.Resumed = false, false
		s.Fails, s.Idle, s.PaneHash = 0, 0, ""
		return
	}
	if len(s.DirectPrompts) > 0 {
		sent, err := e.submitDirect(s, s.DirectPrompts[0])
		if err != nil {
			s.DirectError = "queued direct steer submission failed; delivery uncertain; automatic prompts paused: " + err.Error()
			s.LastErr = s.DirectError
			e.log.Printf("%s: %s", r.Name, s.DirectError)
			return
		}
		if sent {
			s.DirectPrompts = s.DirectPrompts[1:]
			s.Idle, s.PaneHash = 0, ""
			e.log.Printf("%s: queued direct steer submitted (%d remaining)", r.Name, len(s.DirectPrompts))
		}
		return
	}
	out, err := tmux.Capture(sess)
	if err != nil {
		return
	}
	s.LastErr = ""
	if h := space.Hash([]byte(out)); h == s.PaneHash {
		s.Idle++
	} else {
		s.PaneHash, s.Idle = h, 0
	}
	if !e.promptReady(sess, s.Harness) {
		s.Idle = 0
		return
	}
	if s.Idle >= e.cfg.IdleThreshold(r.Name, inboxEmpty(r)) {
		if e.sendWhenReady(sess, s.Harness, e.cfg.Prompt(r.Name, config.PromptNudge), &s.LastReady) {
			e.notice("quiet/"+r.Name, fmt.Sprintf("%s: quiet; nudged (inbox %d)", r.Name, countDir(r.Inbox())))
			s.Idle, s.PaneHash = 0, ""
		}
	}
}

func (e *Engine) roleError(name string, s *roleState, err error) {
	if s.LastErr != err.Error() {
		e.log.Printf("%s: %v", name, err)
		s.LastErr = err.Error()
	}
}

func (e *Engine) hire(r space.Role, s *roleState) {
	cli := e.cfg.HarnessFor(r.Name)
	resume := s.Started && s.Harness == cli
	cmd, err := e.cfg.CommandFor(r.Name, resume)
	if err != nil {
		e.roleError(r.Name, s, err)
		return
	}
	prepared, prepareErr := harness.WithMCP(e.root, r.Name, cli, cmd, e.cfg.MCPEnabled, e.cfg.MCPTimeout)
	if prepareErr != nil {
		e.log.Printf("%s: optional company tools unavailable: %v", r.Name, prepareErr)
	} else {
		cmd = prepared
	}
	next := *s
	next.Session = r.Session(e.cfg.SessionPrefix)
	next.Harness, next.Cmd = cli, strings.Join(cmd, " ")
	next.Started, next.NeedPrompt, next.Resumed = true, true, resume
	next.NeedShake = len(e.cfg.Handshake(r.Name)) > 0
	next.Idle, next.PaneHash, next.LastReady = 0, "", ""
	if err := tmux.New(next.Session, r.Dir, cmd, e.metadata("role", r.Name, &next)); err != nil {
		s.LaunchRetryAt = time.Now().Add(e.cfg.LaunchRetryDelay)
		e.roleError(r.Name, s, fmt.Errorf("%w; retrying after %s", err, e.cfg.LaunchRetryDelay))
		return
	}
	next.LaunchRetryAt, next.LastErr = time.Time{}, ""
	*s = next
	verb := "hired"
	if resume {
		verb = "revived"
	}
	e.log.Printf("%s: %s", r.Name, verb)
}

func (e *Engine) roleDied(name string, s *roleState, sess string) bool {
	out, _ := tmux.Capture(sess)
	status := tmux.DeadStatus(sess)
	if err := e.killSession(sess); err != nil {
		e.roleError(name, s, err)
		return false
	}
	startup := s.NeedPrompt || s.NeedShake
	s.Session = ""
	s.NeedShake, s.NeedPrompt = false, false
	if s.Resumed && startup {
		s.Started, s.Resumed = false, false
		e.log.Printf("%s: resume failed during startup, starting fresh", name)
		return true
	}
	if startup {
		s.Started = false
	}
	s.Resumed = false
	s.Fails++
	switch {
	case s.Fails >= e.cfg.MaxRestarts:
		s.Broken = true
		e.log.Printf("%s: harness exited %d times (last status %s), giving up until role inputs or settings change\n    command: %s\n%s", name, s.Fails, status, s.Cmd, indent(lastLines(out, 8)))
	case s.Fails == 1:
		e.log.Printf("%s: session exited, restarting", name)
	}
	return true
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

func (e *Engine) send(session, text string) bool {
	if text == "" {
		return true
	}
	if err := tmux.SendLine(session, text); err != nil {
		e.notice("send/"+session, fmt.Sprintf("%s: cannot send prompt: %v", session, err))
		return false
	}
	delete(e.notices, "send/"+session)
	return true
}
