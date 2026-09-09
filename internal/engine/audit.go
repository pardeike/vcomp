package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vcomp/internal/space"
)

// The audit trail records every message that passes through an inbox, so a run
// can be analysed afterwards: who asked whom for what, how long it sat there,
// and whether anything came back.
//
// It works by remembering rather than intercepting. Agents create and delete
// inbox folders directly - that is the whole communication design, and putting
// the engine in the middle of it would change the thing being measured. So
// each tick the engine reads the inboxes, writes down anything it has not seen
// before, and notices what has gone. A message the engine has seen once is
// preserved in the log even if its folder is deleted a second later.
//
// The one honest gap: a message created and deleted entirely within a single
// tick is never observed. That window is the tick interval, and a message that
// short-lived was not read by its recipient either.

// event is one line of the trail. Attachments are counted, not copied: the
// prose is what is worth analysing, and artifacts are already in the repo.
type event struct {
	At      time.Time `json:"at"`
	Seq     int       `json:"seq"`
	Event   string    `json:"event"` // sent, updated, cleared
	To      string    `json:"to"`
	From    string    `json:"from,omitempty"`
	Topic   string    `json:"topic"`
	Hash    string    `json:"hash,omitempty"`
	Bytes   int       `json:"bytes,omitempty"`
	Files   int       `json:"files,omitempty"` // attachments beside message.md
	Waiting int       `json:"waiting"`         // how deep that inbox was at the time
	Text    string    `json:"text,omitempty"`
	Lived   string    `json:"lived,omitempty"` // how long it sat there, on clearing
}

// audit walks every inbox, logs what has changed since last tick, and forgets
// what has gone.
func (e *Engine) audit(roles []space.Role) {
	if e.cfg.AuditFile == "" {
		return
	}
	if e.st.Messages == nil {
		e.st.Messages = map[string]*msgState{}
	}
	now := time.Now()
	seen := map[string]bool{}
	var events []event

	for _, r := range roles {
		topics, err := os.ReadDir(r.Inbox())
		if err != nil {
			continue
		}
		for _, topic := range topics {
			if !topic.IsDir() {
				continue
			}
			key := r.Name + "/" + topic.Name()
			seen[key] = true

			body, files := readMessage(filepath.Join(r.Inbox(), topic.Name()))
			hash := space.Hash([]byte(body))
			prev := e.st.Messages[key]
			if prev != nil && prev.Hash == hash {
				continue
			}
			kind := "sent"
			if prev != nil {
				kind = "updated"
			} else {
				e.st.Messages[key] = &msgState{First: now}
			}
			e.st.Messages[key].Hash = hash
			events = append(events, event{
				At: now, Event: kind, To: r.Name, From: sender(body),
				Topic: topic.Name(), Hash: hash, Bytes: len(body), Files: files,
				Waiting: len(topics), Text: body,
			})
		}
	}

	// Anything the engine knew about and can no longer find was dealt with, or
	// abandoned. Either way it leaves the inbox and the trail records when.
	for key, m := range e.st.Messages {
		if seen[key] {
			continue
		}
		to, topic, _ := strings.Cut(key, "/")
		events = append(events, event{
			At: now, Event: "cleared", To: to, Topic: topic, Hash: m.Hash,
			Lived: now.Sub(m.First).Round(time.Second).String(),
		})
		delete(e.st.Messages, key)
	}
	e.append(events)
}

// readMessage returns the prose and how many other files came with it.
func readMessage(dir string) (body string, attachments int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0
	}
	for _, f := range entries {
		if f.Name() == "message.md" {
			if b, err := os.ReadFile(filepath.Join(dir, f.Name())); err == nil {
				body = string(b)
			}
			continue
		}
		attachments++
	}
	return body, attachments
}

// sender reads the "From:" line the conventions ask for. It is a convention,
// not a guarantee, so an unsigned message is recorded without one rather than
// guessed at.
func sender(body string) string {
	for i, line := range strings.SplitN(body, "\n", 6) {
		if i > 4 {
			break
		}
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "From:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// append writes the events as JSON lines, which stay appendable while a run is
// going and are trivial to load afterwards.
func (e *Engine) append(events []event) {
	if len(events) == 0 {
		return
	}
	path := filepath.Join(e.root, e.cfg.AuditFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		e.log.Printf("cannot write the audit trail: %v", err)
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, ev := range events {
		e.st.AuditSeq++
		ev.Seq = e.st.AuditSeq
		if err := enc.Encode(ev); err != nil {
			return
		}
	}
}
