package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHarnessIntegrationScopesAndPreservesConfiguration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "company with spaces")
	if err := os.MkdirAll(filepath.Join(root, "spaces", "ceo", ".omp"), 0755); err != nil {
		t.Fatal(err)
	}
	root, _ = filepath.EvalSymlinks(root)
	ompPath := filepath.Join(root, "spaces", "ceo", ".omp", "mcp.json")
	if err := os.WriteFile(ompPath, []byte(`{"mcpServers":{"other":{"command":"untouched"}},"settings":{"keep":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"local/test","mcp":{"other":{"type":"local","command":["untouched"]}}}`)
	for _, cli := range []string{"omp", "claude", "codex", "pi", "opencode", "custom"} {
		original := []string{cli, "--continue"}
		cmd, err := WithMCP(root, "ceo", cli, original, true, 30*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", cli, err)
		}
		if !reflect.DeepEqual(original, []string{cli, "--continue"}) {
			t.Fatal("mutated input command")
		}
		switch cli {
		case "omp":
			b, _ := os.ReadFile(ompPath)
			var cfg map[string]any
			json.Unmarshal(b, &cfg)
			servers := cfg["mcpServers"].(map[string]any)
			if len(servers) != 2 || cfg["settings"] == nil {
				t.Fatal(string(b))
			}
			args := servers[serverName].(map[string]any)["args"].([]any)
			if args[2] != root || args[4] != "ceo" {
				t.Fatal(args)
			}
		case "claude":
			b, err := os.ReadFile(cmd[len(cmd)-1])
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), root) || cmd[len(cmd)-2] != "--mcp-config" {
				t.Fatal(cmd, string(b))
			}
		case "codex":
			if !strings.Contains(strings.Join(cmd, " "), root) || !strings.Contains(strings.Join(cmd, " "), "mcp_servers.vcomp-company.args=") {
				t.Fatal(cmd)
			}
		case "pi":
			b, err := os.ReadFile(cmd[len(cmd)-1])
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), root) || !strings.Contains(string(b), "tools/list") {
				t.Fatal(cmd)
			}
		case "opencode":
			var cfg map[string]any
			json.Unmarshal([]byte(strings.TrimPrefix(cmd[1], "OPENCODE_CONFIG_CONTENT=")), &cfg)
			if cfg["model"] != "local/test" || len(cfg["mcp"].(map[string]any)) != 2 || !reflect.DeepEqual(cmd[2:], original) {
				t.Fatal(cmd)
			}
		case "custom":
			if !reflect.DeepEqual(cmd, original) {
				t.Fatal(cmd)
			}
		}
		disabled, err := WithMCP(root, "ceo", cli, original, false, 30*time.Second)
		if err != nil || !reflect.DeepEqual(disabled, original) {
			t.Fatal(cli, disabled, err)
		}
	}
	b, _ := os.ReadFile(ompPath)
	if strings.Contains(string(b), serverName) || !strings.Contains(string(b), "untouched") {
		t.Fatal(string(b))
	}
}
func TestMalformedConfigurationRemainsUntouched(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "spaces", "ceo", ".omp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	original := []byte("user editing { incomplete")
	if err := os.WriteFile(p, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := WithMCP(root, "ceo", "omp", []string{"omp"}, true, 30*time.Second); err == nil {
		t.Fatal("accepted malformed config")
	}
	b, _ := os.ReadFile(p)
	if string(b) != string(original) {
		t.Fatal("rewrote user config")
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", "null")
	if _, err := WithMCP(root, "ceo", "opencode", []string{"opencode"}, true, 30*time.Second); err == nil {
		t.Fatal("accepted null config")
	}
}
