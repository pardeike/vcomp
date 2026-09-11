package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func company(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// macOS temporary paths can themselves be symlink aliases.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"ceo", "developer"} {
		dir := filepath.Join(root, "spaces", role)
		if err := os.MkdirAll(filepath.Join(dir, "inbox"), 0755); err != nil {
			t.Fatal(err)
		}
		for file, text := range map[string]string{"role.md": role + " role\n", "role.json": `{"position":"developer"}`, "notes.md": "first\nsecond\nthird\n", "goals.md": "my goals\n"} {
			if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "CONVENTIONS.md"), []byte("company rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}
func call(t *testing.T, s *Server, name string, args object) object {
	t.Helper()
	b, _ := json.Marshal(args)
	value, err := s.Call(name, b)
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}
func server(t *testing.T, root, role string) *Server {
	t.Helper()
	s, err := New(root, role)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMessagesStayInTheirCompanyAndReadsDoNotRemove(t *testing.T) {
	t.Setenv("VCOMP_HOME", t.TempDir())
	roots := []string{company(t), company(t)}
	var wg sync.WaitGroup
	for i, root := range roots {
		wg.Add(1)
		go func(i int, root string) {
			defer wg.Done()
			sender := server(t, root, "ceo")
			recipient := server(t, root, "developer")
			sent := call(t, sender, "message_send", object{"recipient": "developer", "subject": "Art: fish / icon", "body": fmt.Sprintf("only company %d", i)})
			topic := sent["topic"].(string)
			if strings.ContainsAny(topic, ":/\\") || strings.Contains(topic, "FROM USER") {
				t.Errorf("bad employee topic: %s", topic)
			}
			listed := call(t, recipient, "inbox_list", object{})
			if listed["total"] != 1 {
				t.Errorf("cross-company or missing topic: %v", listed)
			}
			read := call(t, recipient, "inbox_read", object{"topic": topic})
			text := strings.Join(read["lines"].([]string), "\n")
			if !strings.Contains(text, "From: ceo") || !strings.Contains(text, fmt.Sprintf("only company %d", i)) {
				t.Errorf("wrong contents: %s", text)
			}
			if _, err := os.Stat(filepath.Join(root, "spaces", "developer", "inbox", topic)); err != nil {
				t.Error("read removed topic", err)
			}
			_, err := sender.Call("inbox_remove", json.RawMessage(fmt.Sprintf(`{"role":"developer","topic":%q}`, topic)))
			if err == nil {
				t.Error("remove accepted another employee")
			}
			call(t, recipient, "inbox_remove", object{"topic": topic})
			if call(t, recipient, "inbox_list", object{})["total"] != 0 {
				t.Error("remove failed")
			}
		}(i, root)
	}
	wg.Wait()
}

func TestPagesAndValidation(t *testing.T) {
	s := server(t, company(t), "ceo")
	page := call(t, s, "role_read", object{"document": "notes", "limit": 2})
	if strings.Join(page["lines"].([]string), ",") != "first,second" || page["next_offset"] != 2 {
		t.Fatal(page)
	}
	page = call(t, s, "role_read", object{"document": "notes", "offset": 2, "limit": 2})
	if strings.Join(page["lines"].([]string), ",") != "third" || page["next_offset"] != nil {
		t.Fatal(page)
	}
	for _, args := range []string{`null`, `[]`, `{"role":"../ceo"}`, `{"limit":0}`, `{"limit":101}`, `{"offset":-1}`, `{"offset":null}`, `{"role":1}`, `{"Role":"developer"}`, `{"invented":true}`} {
		if _, err := s.Call("inbox_list", json.RawMessage(args)); err == nil {
			t.Errorf("accepted %s", args)
		}
	}
	for _, doc := range []string{"missing", "../role"} {
		if _, err := s.Call("role_read", json.RawMessage(fmt.Sprintf(`{"document":%q}`, doc))); err == nil {
			t.Error("accepted document", doc)
		}
	}
	for _, topic := range []string{"..", "../developer", "a/b", "a\\b"} {
		if _, err := s.Call("inbox_remove", json.RawMessage(fmt.Sprintf(`{"topic":%q}`, topic))); err == nil {
			t.Error("accepted topic", topic)
		}
	}
	if _, err := s.Call("message_send", json.RawMessage(`{"recipient":"invented","subject":"test","body":"hi"}`)); err == nil {
		t.Fatal("invented recipient accepted")
	}
	if err := os.WriteFile(filepath.Join(s.Root, "spaces", "ceo", "notes.md"), []byte(strings.Repeat("魚", 1100)), 0644); err != nil {
		t.Fatal(err)
	}
	page = call(t, s, "role_read", object{"document": "notes"})
	if !strings.Contains(page["lines"].([]string)[0], "line clipped") {
		t.Fatal("long line not marked as clipped")
	}
}

func TestAliasesCannotRedirectInboxMutations(t *testing.T) {
	root := company(t)
	other := company(t)
	s := server(t, root, "ceo")
	alias := filepath.Join(t.TempDir(), "company alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if server(t, alias, "ceo").Root != root {
		t.Fatal("root not canonical")
	}
	inbox := filepath.Join(root, "spaces", "developer", "inbox")
	if err := os.Remove(inbox); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.Join(other, "spaces", "developer", "inbox"), filepath.Join(root, "spaces", "ceo", "inbox")} {
		if err := os.Symlink(target, inbox); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Call("message_send", json.RawMessage(`{"recipient":"developer","subject":"wrong alias","body":"hello"}`)); err == nil {
			t.Fatal("sent through alias", target)
		}
		if err := os.Remove(inbox); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStdioLifecycle(t *testing.T) {
	s := server(t, company(t), "ceo")
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}
{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"role_read","arguments":{"document":"conventions"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"inbox_remove","arguments":{"topic":"missing"}}}
{"jsonrpc":"2.0","id":5,"method":"not-a-method"}
malformed
{"jsonrpc":"2.0","id":6,"method":"ping"}
`)
	var out bytes.Buffer
	if err := s.Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("stdout has unexpected lines: %s", out.String())
	}
	replies := []object{}
	for _, line := range lines {
		var reply object
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatal("protocol noise", line)
		}
		replies = append(replies, reply)
	}
	if replies[0]["error"] == nil {
		t.Fatal("list before initialize succeeded")
	}
	if replies[1]["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal(replies[1])
	}
	tools := replies[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 6 {
		t.Fatal(tools)
	}
	for _, tool := range tools {
		spec := tool.(map[string]any)["inputSchema"].(map[string]any)
		if required, ok := spec["required"]; ok && required == nil {
			t.Fatal("null required schema")
		}
	}
	if !strings.Contains(lines[3], "company rules") {
		t.Fatal(lines[3])
	}
	if replies[4]["result"].(map[string]any)["isError"] != true {
		t.Fatal(replies[4])
	}
	if replies[5]["error"] == nil || replies[6]["error"] == nil || replies[7]["result"] == nil {
		t.Fatal(replies)
	}
}

func TestOverviewIdentifiesCompanyWithoutReadingProductSources(t *testing.T) {
	t.Setenv("VCOMP_HOME", t.TempDir())
	s := server(t, company(t), "developer")
	result := call(t, s, "company_overview", object{"limit": 1})
	if result["company_root"] != s.Root || result["self"] != "developer" || result["employees_total"] != 2 || result["next_offset"] != 1 {
		t.Fatal(result)
	}
	employees := result["employees"].([]object)
	if len(employees) != 1 || employees[0]["profession"] != "developer" {
		t.Fatal(result)
	}
}
