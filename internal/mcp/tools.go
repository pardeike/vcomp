package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"vcomp/internal/bootstrap"
	"vcomp/internal/engine"
)

func (s *Server) Call(name string, raw json.RawMessage) (any, error) {
	a, err := parseArgs(name, raw)
	if err != nil {
		return nil, err
	}
	if _, err := s.employee(s.Role); err != nil {
		return nil, err
	}
	if a.Role == "" {
		a.Role = s.Role
	}
	switch name {
	case "company_overview":
		return s.overview(a)
	case "role_read":
		files := map[string]string{"role": "role.md", "conventions": "CONVENTIONS.md", "notes": "notes.md", "goals": "goals.md"}
		file, ok := files[a.Document]
		if !ok {
			return nil, fmt.Errorf("document must be role, conventions, notes or goals")
		}
		dir, err := s.employee(a.Role)
		if err != nil {
			return nil, err
		}
		if a.Document == "conventions" {
			dir = s.Root
		}
		rel, _ := filepath.Rel(s.Root, filepath.Join(dir, file))
		p, err := s.path(rel)
		if err != nil {
			return nil, err
		}
		return readPage(p, a.Offset, a.Limit)
	case "inbox_list":
		dir, err := s.employee(a.Role)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(s.Root, filepath.Join(dir, "inbox"))
		inbox, err := s.ordinaryDir(rel)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(inbox)
		if err != nil {
			return nil, err
		}
		visible := entries[:0]
		for _, entry := range entries {
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				visible = append(visible, entry)
			}
		}
		start, end := pageBounds(len(visible), a.Offset, a.Limit)
		topics := []object{}
		for _, entry := range visible[start:end] {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			preview := ""
			p, err := s.topic(a.Role, entry.Name())
			if err != nil {
				continue
			}
			msg, err := s.path(p, "message.md")
			if err == nil {
				f, err := os.Open(msg)
				if err == nil {
					b, _ := io.ReadAll(io.LimitReader(f, 240))
					f.Close()
					preview = compact(string(b), 180)
				}
			}
			topics = append(topics, object{"topic": entry.Name(), "preview": preview, "modified_at": info.ModTime().UTC().Format(time.RFC3339)})
		}
		return object{"role": a.Role, "topics": topics, "total": len(visible), "next_offset": nextOffset(end, len(visible))}, nil
	case "inbox_read":
		dir, err := s.topic(a.Role, a.Topic)
		if err != nil {
			return nil, err
		}
		p, err := s.path(dir, "message.md")
		if err != nil {
			return nil, err
		}
		result, err := readPage(p, a.Offset, a.Limit)
		if err != nil {
			return nil, err
		}
		absolute, err := s.path(dir)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(absolute)
		if err != nil {
			return nil, err
		}
		attachments := []string{}
		for _, entry := range entries {
			if entry.Name() != "message.md" {
				attachments = append(attachments, entry.Name())
			}
		}
		result["topic"], result["attachments"] = a.Topic, attachments[:min(100, len(attachments))]
		result["attachments_total"] = len(attachments)
		return result, nil
	case "message_send":
		if len(a.Body) > 64*1024 {
			return nil, fmt.Errorf("message exceeds 64 KiB; put large artifacts in files and refer to them")
		}
		dir, err := s.employee(a.Recipient)
		if err != nil {
			return nil, err
		}
		// Resolve the existing inbox before publishing, including symlink scope.
		rel, _ := filepath.Rel(s.Root, filepath.Join(dir, "inbox"))
		if _, err := s.ordinaryDir(rel); err != nil {
			return nil, err
		}
		path, err := bootstrap.EmployeeMessage(s.Root, s.Role, a.Recipient, a.Subject, a.Body)
		if err != nil {
			return nil, err
		}
		return object{"recipient": a.Recipient, "from": s.Role, "topic": filepath.Base(path), "path": filepath.Join(path, "message.md"), "status": "written; receipt does not imply read"}, nil
	case "inbox_remove":
		rel, err := s.topic(s.Role, a.Topic)
		if err != nil {
			return nil, err
		}
		p, err := s.path(rel)
		if err != nil {
			return nil, err
		}
		if err := os.RemoveAll(p); err != nil {
			return nil, err
		}
		return object{"role": s.Role, "topic": a.Topic, "status": "removed"}, nil
	}
	return nil, fmt.Errorf("unknown tool")
}
func (s *Server) topic(role, topic string) (string, error) {
	if topic == "" || topic == "." || topic == ".." || filepath.Base(topic) != topic || strings.ContainsAny(topic, "/\\\x00") {
		return "", fmt.Errorf("use one exact topic name from inbox_list")
	}
	if _, err := s.employee(role); err != nil {
		return "", err
	}
	rel := filepath.Join("spaces", role, "inbox", topic)

	if _, err := s.ordinaryDir(rel); err != nil {
		return "", err
	}
	return rel, nil
}
func compact(s string, n int) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	if utf8.RuneCountInString(s) > n {
		return string([]rune(s)[:n]) + "…"
	}
	return s
}
func pageBounds(total, offset, limit int) (int, int) {
	start := min(offset, total)
	return start, min(total, start+limit)
}
func nextOffset(end, total int) any {
	if end < total {
		return end
	}
	return nil
}
func readPage(path string, offset, limit int) (object, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	lines := []string{}
	index := 0
	more := false
	for scanner.Scan() {
		if index >= offset {
			if len(lines) >= limit {
				more = true
				break
			}
			lines = append(lines, compactLine(scanner.Text(), 1000))
		}
		index++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	var next any
	if more {
		next = offset + len(lines)
	}
	return object{"path": path, "offset": offset, "lines": lines, "next_offset": next, "long_lines_clipped_at": 1000}, nil
}
func compactLine(s string, n int) string {
	r := []rune(strings.ToValidUTF8(s, ""))
	if len(r) > n {
		return string(r[:n]) + "… [line clipped; read the file directly for full text]"
	}
	return string(r)
}

func (s *Server) overview(a arguments) (any, error) {
	v := engine.Observe(s.Root)
	employees := []object{}
	start, end := pageBounds(len(v.Agents), a.Offset, a.Limit)
	for _, r := range v.Agents[start:end] {
		item := object{"name": r.Name, "state": r.State, "inbox": r.Inbox, "harness": r.Harness}
		var definition struct {
			Position string `json:"position"`
		}
		if b, err := os.ReadFile(filepath.Join(s.Root, "spaces", r.Name, "role.json")); err == nil && json.Unmarshal(b, &definition) == nil && definition.Position != "" {
			item["profession"] = definition.Position
		}
		if r.Error != "" {
			item["error"] = compact(r.Error, 320)
		}
		if r.Turns.Known {
			item["completed_turns"] = r.Turns.Completed
			if !r.Turns.Started.IsZero() {
				item["turn_started_at"] = r.Turns.Started.UTC().Format(time.RFC3339)
			}
			if r.Turns.Completed > 0 {
				item["average_turn_seconds"] = int(r.Turns.Average.Seconds())
			}
			if r.Turns.Activity != "" {
				lines := strings.Split(strings.TrimSpace(r.Turns.Activity), "\n")
				item["latest_record"] = compact(lines[len(lines)-1], 160)
			}
		}
		employees = append(employees, item)
	}
	commits := []string{}
	cmd := exec.Command("git", "-C", filepath.Join(s.Root, "product"), "log", "-3", "--format=%h %s")
	if b, err := cmd.Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line != "" {
				commits = append(commits, compact(line, 160))
			}
		}
	}
	runs := []object{}
	for _, r := range v.Runs[max(0, len(v.Runs)-5):] {
		runs = append(runs, object{"name": r.Name, "state": r.State})
	}
	result := object{"company_root": s.Root, "self": s.Role, "space": filepath.Join(s.Root, "spaces", s.Role), "product": filepath.Join(s.Root, "product"), "conventions": filepath.Join(s.Root, "CONVENTIONS.md"), "supervised": v.Supervised, "employees": employees, "employees_total": len(v.Agents), "next_offset": nextOffset(end, len(v.Agents)), "recent_commits": commits, "recent_public_runs": runs, "public_runs_total": len(v.Runs), "queried_at": time.Now().UTC().Format(time.RFC3339)}
	if !v.ObservedAt.IsZero() {
		result["engine_observed_at"] = v.ObservedAt.UTC().Format(time.RFC3339)
	}
	if v.Error != "" {
		result["observation_error"] = compact(v.Error, 320)
	}
	return result, nil
}
