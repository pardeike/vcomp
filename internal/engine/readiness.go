package engine

import (
	"regexp"
	"strings"
	"vcomp/internal/config"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
)

// Silence is not readiness. Require a recognized input prompt and reject busy,
// queue, retry and dialog indicators, even if every captured frame is identical.
func inputReady(h config.Harness, output string) bool {
	if h.ReadyPattern == "" {
		return false
	}
	output = strings.ReplaceAll(output, "\u00a0", " ")
	ready, err := regexp.Compile(h.ReadyPattern)
	if err != nil || !ready.MatchString(output) {
		return false
	}
	if h.BusyPattern != "" {
		busy, err := regexp.Compile(h.BusyPattern)
		if err != nil || busy.MatchString(output) {
			return false
		}
	}
	return true
}
func (e *Engine) promptReady(session, harness string) bool {
	h := e.cfg.Harnesses[harness]
	if h.ReadyPattern == "" {
		e.notice("readiness/"+session, session+": automatic prompts paused; harness has no ready_pattern")
		return false
	}
	output, err := tmux.Capture(session)
	return err == nil && inputReady(h, output)
}

// Latch a submitted ready frame, including across supervisor restarts. A slow
// UI must acknowledge input by changing before another prompt can be submitted.
func (e *Engine) sendWhenReady(session, harness, text string, lastReady *string) bool {
	output, err := tmux.Capture(session)
	if err != nil || !inputReady(e.cfg.Harnesses[harness], output) {
		return false
	}
	hash := space.Hash([]byte(output))
	if hash == *lastReady {
		return false
	}
	if !e.send(session, text) {
		return false
	}
	*lastReady = hash
	return true
}
