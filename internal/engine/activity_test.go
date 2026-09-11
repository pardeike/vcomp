package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActivityKeepsToolsAndResultsWithoutDumpingThinking(t *testing.T) {
	raw := json.RawMessage(`[{"type":"thinking","thinking":"private scratch"},{"type":"toolCall","name":"bash","intent":"Inspect manifest","arguments":{"command":"cat Package.swift"}}]`)
	brief, detail := activityText("assistant", "", "toolUse", raw)
	if !strings.Contains(brief, "Inspect manifest") || strings.Contains(brief, "cat Package") || !strings.Contains(detail, "cat Package.swift") || strings.Contains(detail, "private scratch") {
		t.Fatal(brief, detail)
	}
	brief, detail = activityText("toolResult", "bash", "", json.RawMessage(`[{"type":"text","text":"first line\nsecond line"}]`))
	if strings.Contains(brief, "second line") || !strings.Contains(detail, "second line") {
		t.Fatal(brief, detail)
	}
	brief, _ = activityText("assistant", "", "aborted", nil)
	if brief != "Response aborted" {
		t.Fatal(brief)
	}
}
