package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func turnEntry(role, reason string, seconds int) string {
	return fmt.Sprintf("{\"type\":\"message\",\"timestamp\":\"2026-09-11T08:00:%02dZ\",\"message\":{\"role\":%q,\"stopReason\":%q}}\n", seconds, role, reason)
}

func TestTurnStatsIncludeToolsAndExcludeUnfinishedTurns(t *testing.T) {
	cwd := t.TempDir()
	header := fmt.Sprintf("{\"type\":\"title\"}\n{\"type\":\"session\",\"cwd\":%q}\n", cwd)
	transcript := header + turnEntry("user", "", 0) + turnEntry("assistant", "toolUse", 5) +
		turnEntry("toolResult", "", 8) + turnEntry("user", "", 10) + // steering within the same turn
		turnEntry("assistant", "stop", 20) + turnEntry("user", "", 25) +
		turnEntry("assistant", "stop", 35) + turnEntry("user", "", 40)
	s := parseTurnStats(strings.NewReader(transcript), cwd)
	if !s.Known || s.Completed != 2 || s.Average != 15*time.Second || s.Started.Second() != 40 {
		t.Fatalf("incorrect assignment turns: %+v", s)
	}
	for _, reason := range []string{"error", "aborted"} {
		closed := parseTurnStats(strings.NewReader(transcript+turnEntry("assistant", reason, 45)), cwd)
		if closed.Completed != 2 || closed.Average != 15*time.Second || !closed.Started.IsZero() {
			t.Fatalf("%s counted as completed: %+v", reason, closed)
		}
	}
	retried := parseTurnStats(strings.NewReader(header+turnEntry("user", "", 0)+
		turnEntry("assistant", "error", 5)+turnEntry("assistant", "toolUse", 10)+
		turnEntry("assistant", "stop", 20)), cwd)
	if retried.Completed != 1 || retried.Average != 20*time.Second {
		t.Fatalf("automatic retry lost assignment start: %+v", retried)
	}
	partial := parseTurnStats(strings.NewReader(transcript+`{"type":"message"`), cwd)
	if partial != s {
		t.Fatalf("partial append lost recorded state: %+v", partial)
	}
	if parseTurnStats(strings.NewReader(transcript), t.TempDir()).Known {
		t.Fatal("accepted another employee's conversation")
	}
}

func TestTurnHistoryTracksTerminalSessionAndUpdates(t *testing.T) {
	cwd, history := t.TempDir(), t.TempDir()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	breadcrumb := filepath.Join(history, "ttys999")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(breadcrumb, cwd+"\n"+path+"\nfresh\n")
	if s := readTurnStats(history, "/dev/ttys999", cwd); !s.Known || s.Completed != 0 {
		t.Fatalf("fresh session is not empty: %+v", s)
	}
	header := fmt.Sprintf("{\"type\":\"session\",\"cwd\":%q}\n", cwd)
	write(path, header+turnEntry("user", "", 1))
	s := readTurnStats(history, "/dev/ttys999", cwd)
	if !s.Known || s.Started.IsZero() {
		t.Fatalf("missing active turn: %+v", s)
	}
	if readTurnStats(history, "/dev/ttys999", cwd) != s {
		t.Fatal("unchanged transcript changed statistics")
	}
	write(path, header+turnEntry("user", "", 1)+turnEntry("assistant", "stop", 11))
	if s := readTurnStats(history, "/dev/ttys999", cwd); s.Completed != 1 || s.Average != 10*time.Second || !s.Started.IsZero() {
		t.Fatalf("append was not observed: %+v", s)
	}
	if readTurnStats(history, "/dev/ttys999", t.TempDir()).Known {
		t.Fatal("stale terminal breadcrumb matched wrong employee")
	}
	other := filepath.Join(t.TempDir(), "new.jsonl")
	write(other, header)
	write(breadcrumb, cwd+"\n"+other+"\n")
	if s := readTurnStats(history, "/dev/ttys999", cwd); !s.Known || s.Completed != 0 || !s.Started.IsZero() {
		t.Fatalf("replacement inherited old counters: %+v", s)
	}
}
