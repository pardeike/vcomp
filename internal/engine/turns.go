package engine

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TurnStats describes completed prompt-to-final-response turns in the active
// OMP conversation. Tool calls are part of the turn, not additional turns.
type TurnStats struct {
	Activity, Detail string
	Latest           time.Time
	Known            bool
	Completed        int
	Started          time.Time
	Average          time.Duration
}

// Cache unchanged transcripts by terminal breadcrumb so dashboard refreshes
// neither reread large conversations nor accumulate every old session file.
var turnCache = struct {
	sync.Mutex
	entries map[string]turnCacheEntry
}{entries: make(map[string]turnCacheEntry)}

type turnCacheEntry struct {
	path, cwd string
	size      int64
	modified  time.Time
	stats     TurnStats
}

func canonicalDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(dir)
}

func readTurnStats(history, tty, cwd string) TurnStats {
	if history == "" {
		return TurnStats{}
	}
	if strings.HasPrefix(history, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return TurnStats{}
		}
		history = filepath.Join(home, history[2:])
	}
	breadcrumb := filepath.Join(history, filepath.Base(tty))
	b, err := os.ReadFile(breadcrumb)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	cwd = canonicalDir(cwd)
	if err != nil || len(lines) < 2 || canonicalDir(lines[0]) != cwd {
		return TurnStats{}
	}
	path := lines[1]
	info, err := os.Stat(path)
	if os.IsNotExist(err) && len(lines) > 2 && lines[2] == "fresh" {
		return TurnStats{Known: true}
	}
	if err != nil || !info.Mode().IsRegular() {
		return TurnStats{}
	}
	turnCache.Lock()
	defer turnCache.Unlock()
	cached := turnCache.entries[breadcrumb]
	if cached.path == path && cached.cwd == cwd && cached.size == info.Size() && cached.modified.Equal(info.ModTime()) {
		return cached.stats
	}
	f, err := os.Open(path)
	if err != nil {
		return TurnStats{}
	}
	defer f.Close()
	stats := parseTurnStats(f, cwd)
	turnCache.entries[breadcrumb] = turnCacheEntry{path, cwd, info.Size(), info.ModTime(), stats}
	return stats
}

func parseTurnStats(input io.Reader, cwd string) TurnStats {
	cwd = canonicalDir(cwd)
	r := bufio.NewReader(input)
	stats := TurnStats{}
	var total time.Duration
	var retryStart time.Time
	for {
		line, err := r.ReadBytes('\n')
		// Writers append JSONL records. An incomplete final line is still in
		// flight; preserve the last fully recorded state until the next refresh.
		if err == io.EOF {
			return stats
		}
		if err != nil {
			return TurnStats{}
		}
		var entry struct {
			Type, Cwd string
			Timestamp time.Time
			Message   struct {
				Role, StopReason string
				Timestamp        int64
				ToolName         string
				Content          json.RawMessage
			}
		}
		if json.Unmarshal(line, &entry) != nil {
			return TurnStats{}
		}
		if entry.Type == "session" {
			if canonicalDir(entry.Cwd) != cwd {
				return TurnStats{}
			}
			stats.Known = true
		}
		if !stats.Known || entry.Type != "message" || entry.Timestamp.IsZero() {
			continue
		}
		brief, detail := activityText(entry.Message.Role, entry.Message.ToolName, entry.Message.StopReason, entry.Message.Content)
		if brief != "" {
			stamp := entry.Timestamp.Local().Format("15:04:05")
			stats.Activity = keepRecent(stamp + "  " + brief + "\n\n" + stats.Activity)
			stats.Detail = keepRecent(stamp + "  " + detail + "\n\n" + stats.Detail)
			stats.Latest = entry.Timestamp
		}
		switch entry.Message.Role {
		case "user":
			retryStart = time.Time{}
			// Steering supplied during a turn belongs to the ongoing turn.
			if stats.Started.IsZero() {
				stats.Started = entry.Timestamp
				if entry.Message.Timestamp > 0 {
					stats.Started = time.UnixMilli(entry.Message.Timestamp)
				}
			}
		case "assistant":
			// An error record can be followed by OMP's automatic retry. Once
			// continuation is recorded, retain the original assignment start.
			if stats.Started.IsZero() && !retryStart.IsZero() && entry.Message.StopReason != "error" && entry.Message.StopReason != "aborted" {
				stats.Started, retryStart = retryStart, time.Time{}
			}
			switch entry.Message.StopReason {
			case "stop", "length":
				if !stats.Started.IsZero() && !entry.Timestamp.Before(stats.Started) {
					total += entry.Timestamp.Sub(stats.Started)
					stats.Completed++
					stats.Average = total / time.Duration(stats.Completed)
				}
				stats.Started = time.Time{}
			case "error":
				if !stats.Started.IsZero() {
					retryStart = stats.Started
				}
				stats.Started = time.Time{}
			case "aborted":
				stats.Started, retryStart = time.Time{}, time.Time{}
			}
		}
	}
}
