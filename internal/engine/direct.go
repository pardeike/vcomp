package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

// DirectRequest is user intervention in an existing conversation, not role editing.
type DirectRequest struct {
	Role string
	All  bool
	Mode string
	Text string
}
type DirectResult struct{ Role, Status string }
type controlCall struct {
	request DirectRequest
	reply   chan []DirectResult
}

func controlPath(root string) string {
	root, _ = filepath.Abs(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	// Unix socket paths have a short platform limit; company paths need not.
	return filepath.Join("/tmp", fmt.Sprintf("vcomp-control-%d-%s.sock", os.Getuid(), space.Hash([]byte(root))))
}

// ListenControl is called only while holding the supervisor lock. The socket
// transports requests; only Run's event loop may touch sessions or engine state.
func (e *Engine) ListenControl() (func(), error) {
	if e.lockF == nil {
		return nil, fmt.Errorf("direct steering requires the supervisor lock")
	}
	path := controlPath(e.root)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	e.controls = make(chan controlCall)
	done := make(chan struct{})
	timeout := e.cfg.DirectSteerTimeout + e.cfg.StopTimeout
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(timeout))
				var req DirectRequest
				if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil {
					return
				}
				call := controlCall{req, make(chan []DirectResult, 1)}
				timer := time.NewTimer(timeout)
				defer timer.Stop()
				select {
				case e.controls <- call:
				case <-timer.C:
					return
				case <-done:
					return
				}
				select {
				case <-timer.C:
					return
				case result := <-call.reply:
					_ = json.NewEncoder(conn).Encode(result)
				case <-done:
				}
			}()
		}
	}()
	return func() { close(done); listener.Close() }, nil
}

// RequestDirect never writes into a harness itself, so it cannot race nudging.
func RequestDirect(root string, req DirectRequest) ([]DirectResult, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", controlPath(root), cfg.StopTimeout)
	if err != nil {
		return nil, fmt.Errorf("direct steering needs a running supervisor with this feature; start or restart vcomp: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(cfg.DirectSteerTimeout + cfg.StopTimeout))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	var results []DirectResult
	if err := json.NewDecoder(conn).Decode(&results); err != nil {
		return nil, fmt.Errorf("delivery outcome unknown; inspect the activity log before retrying: %w", err)
	}
	return results, nil
}

func directText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("a prompt is required")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return "", fmt.Errorf("prompt contains terminal control characters")
		}
	}
	// Prefix avoids treating user prose beginning with / or ! as a CLI command.
	return "FROM USER: " + strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(text), nil
}

func interruptible(h config.Harness, out string) bool {
	if len(h.Interrupt) == 0 || h.InterruptPattern == "" {
		return false
	}
	re, err := regexp.Compile(h.InterruptPattern)
	return err == nil && re.MatchString(strings.ReplaceAll(out, "\u00a0", " "))
}

// This entire operation runs on the supervisor event loop. Automatic prompts
// cannot run between interruption and submission, including for broadcasts.
func (e *Engine) directSteer(req DirectRequest, stop <-chan struct{}) []DirectResult {
	fail := func(err error) []DirectResult { return []DirectResult{{req.Role, "failed: " + err.Error()}} }
	text, err := directText(req.Text)
	if err != nil {
		return fail(err)
	}
	if req.Mode != "queued" && req.Mode != "immediate" {
		return fail(fmt.Errorf("mode must be queued or immediate"))
	}
	if req.All == (req.Role != "") {
		return fail(fmt.Errorf("choose one employee or -all"))
	}
	e.reload()
	if _, done := e.Result(); done {
		return fail(fmt.Errorf("company is finished"))
	}
	names := []string{req.Role}
	if req.All {
		names = nil
		for name, s := range e.st.Roles {
			if p, err := tmux.Inspect(s.Session); err == nil && p.Exists && !p.Dead && sessionRoot(s.Session) == e.root {
				names = append(names, name)
			}
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		return fail(fmt.Errorf("no running employees"))
	}
	roles, err := space.Roles(e.root)
	if err != nil {
		return fail(err)
	}
	results := make([]DirectResult, len(names))
	waiting := map[int]*roleState{}
	clearRestored := map[int]bool{}
	// Interrupt every broadcast target before waiting for any one to settle.
	for i, name := range names {
		results[i].Role = name
		s := e.st.Roles[name]
		if s == nil || req.Mode == "immediate" && (s.NeedPrompt || s.NeedShake) {
			results[i].Status = "failed: employee has no established conversation"
			continue
		}
		current := false
		for _, r := range roles {
			if r.Name == name && r.Hash == s.RoleHash {
				current = true
				break
			}
		}
		if !current {
			results[i].Status = "failed: employee is missing or its role changed"
			continue
		}
		p, err := tmux.Inspect(s.Session)
		if err != nil || !p.Exists || p.Dead || sessionRoot(s.Session) != e.root {
			results[i].Status = "failed: employee session is not running"
			continue
		}
		if req.Mode == "queued" {
			s.DirectError, s.LastErr = "", ""
			s.DirectPrompts = append(s.DirectPrompts, text)
			e.remember(s.Session, "role", name, s)
			results[i].Status = fmt.Sprintf("queued (%d pending)", len(s.DirectPrompts))
			continue
		}
		out, err := tmux.Capture(s.Session)
		if err != nil {
			results[i].Status = "failed: " + err.Error()
			continue
		}
		h := e.cfg.Harnesses[s.Harness]
		if !inputReady(h, out) {
			if !interruptible(h, out) {
				results[i].Status = "failed: unrecognized interruptible state; use interactive intervention"
				continue
			}
			s.DirectError = "direct steer awaiting input; automatic prompts paused"
			e.remember(s.Session, "role", name, s)
			if err := tmux.SendKeys(s.Session, h.Interrupt); err != nil {
				results[i].Status = "failed: " + err.Error()
				continue
			}
			clearRestored[i] = true
		}
		s.DirectError = "direct steer awaiting input; automatic prompts paused"
		e.remember(s.Session, "role", name, s)
		waiting[i] = s
	}
	deadline := time.Now().Add(e.cfg.DirectSteerTimeout)
delivery:
	for len(waiting) > 0 {
		select {
		case <-stop:
			break delivery
		default:
		}
		for i, s := range waiting {
			if clearRestored[i] {
				h := e.cfg.Harnesses[s.Harness]
				out, err := tmux.Capture(s.Session)
				if err == nil && restoredInput(h, out) {
					clearRestored[i] = false
					if err := tmux.SendKeys(s.Session, h.InterruptClear); err != nil {
						e.log.Printf("%s: cannot clear restored input: %v", names[i], err)
					}
				}
			}
			sent, err := e.submitDirect(s, text)
			if err != nil {
				s.DirectError = "direct steer submission failed; delivery uncertain; automatic prompts paused: " + err.Error()
				s.LastErr = s.DirectError
				e.remember(s.Session, "role", names[i], s)
				results[i].Status = "failed: " + s.DirectError
				delete(waiting, i)
				continue
			}
			if sent {
				s.DirectError, s.LastErr = "", ""
				s.Idle, s.PaneHash = 0, ""
				e.remember(s.Session, "role", names[i], s)
				results[i].Status = "submitted to the existing conversation"
				delete(waiting, i)
			}
		}
		if len(waiting) == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(e.cfg.DirectSteerPoll)
		select {
		case <-timer.C:
		case <-stop:
			timer.Stop()
			deadline = time.Now()
		}
	}
	for i, s := range waiting {
		s.DirectError = "direct steer failed: no available prompt; automatic prompts paused. Use direct steer again or interactive intervention, then retry direct steer."
		s.LastErr = s.DirectError
		e.remember(s.Session, "role", names[i], s)
		results[i].Status = "failed: instruction not submitted; " + s.DirectError
	}
	e.saveState()
	for _, result := range results {
		e.log.Printf("%s: direct steer %s: %s", result.Role, req.Mode, result.Status)
	}
	return results
}

// A failed tmux submission may have typed part of the prompt. Do not retry it
// automatically or turn a transport failure into repeated user instructions.
func (e *Engine) submitDirect(s *roleState, text string) (bool, error) {
	out, err := tmux.Capture(s.Session)
	if err != nil {
		return false, err
	}
	if !inputReady(e.cfg.Harnesses[s.Harness], out) {
		return false, nil
	}
	hash := space.Hash([]byte(out))
	if hash == s.LastReady {
		return false, nil
	}
	s.LastReady = hash
	if err := tmux.SendLine(s.Session, text); err != nil {
		return false, err
	}
	return true, nil
}

func restoredInput(h config.Harness, out string) bool {
	if len(h.InterruptClear) == 0 || h.InterruptInputPattern == "" {
		return false
	}
	out = strings.ReplaceAll(out, "\u00a0", " ")
	if h.BusyPattern != "" {
		re, err := regexp.Compile(h.BusyPattern)
		if err != nil || re.MatchString(out) {
			return false
		}
	}
	re, err := regexp.Compile(h.InterruptInputPattern)
	return err == nil && re.MatchString(out)
}
