package engine

import (
	"encoding/json"
	"strings"
)

// Bound display telemetry like other document previews. Keep the latest records in chronological order.
func keepRecent(text string) string {
	const limit = 256 * 1024
	if len(text) > limit {
		tail := text[len(text)-limit:]
		if i := strings.Index(tail, "\n"); i >= 0 {
			tail = tail[i+1:]
		}
		return "[Older activity omitted]\n" + tail
	}
	return text
}
func activityText(role, tool, stop string, raw json.RawMessage) (string, string) {
	var blocks []struct {
		Type, Text, Name, Intent string
		Arguments                json.RawMessage
	}
	if len(raw) > 0 && json.Unmarshal(raw, &blocks) != nil {
		return "", ""
	}
	var brief, detail []string
	for _, b := range blocks {
		switch b.Type {
		case "toolCall":
			label := "Tool: " + b.Name
			if b.Intent != "" {
				label += " · " + b.Intent
			}
			brief = append(brief, label)
			detail = append(detail, label+"\n"+string(b.Arguments))
		case "text":
			text := strings.TrimSpace(b.Text)
			if text == "" {
				continue
			}
			label := role
			if role == "toolResult" {
				label = "Result: " + tool
			}
			line, _, _ := strings.Cut(text, "\n")
			brief = append(brief, label+": "+line)
			detail = append(detail, label+":\n"+text)
		}
	}
	if stop == "aborted" || stop == "error" {
		brief = append(brief, "Response "+stop)
		detail = append(detail, "Response "+stop)
	}
	return strings.Join(brief, "\n"), strings.Join(detail, "\n")
}
