package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vcomp/internal/config"
)

func TestUserMessageIsAnOrdinaryUniqueInboxRequest(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	dir := filepath.Join(root, "spaces", "ceo")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "role.md"), []byte("CEO unchanged"), 0644)
	first, err := Message(root, "ceo", "Designer: late / arrival?", "Please coordinate with the new designer.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(first), "URGENT - FROM USER - ") || strings.ContainsAny(filepath.Base(first), ":/?") {
		t.Fatal(first)
	}
	b, err := os.ReadFile(filepath.Join(first, "message.md"))
	if err != nil || !strings.Contains(string(b), "# FROM USER") || !strings.Contains(string(b), "Please coordinate") {
		t.Fatal(string(b), err)
	}
	second, err := Message(root, "ceo", "Designer: late / arrival?", "Another message")
	if err != nil || second == first {
		t.Fatal("message collision", err)
	}
	for _, name := range []string{"../escape", "missing"} {
		if _, err := Message(root, name, "Subject", "Body"); err == nil {
			t.Fatal("accepted invalid recipient")
		}
	}
	if _, err := Message(root, "ceo", "Subject", ""); err == nil {
		t.Fatal("accepted empty body")
	}
	unchanged, _ := os.ReadFile(filepath.Join(dir, "role.md"))
	if string(unchanged) != "CEO unchanged" {
		t.Fatal("message changed role")
	}
}
