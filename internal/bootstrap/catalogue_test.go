package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vcomp/internal/config"
)

const catalogueDefinition = "Title: Graphic Designer\nSector: creative-media\n\n## Your remit\nMake visual communication clear.\n\n## Your bias\nYou notice typography before slogans.\n"

func TestCatalogueScopesDeletionAndRestore(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	if err := SavePosition(root, "global", "graphic-designer", catalogueDefinition); err != nil {
		t.Fatal(err)
	}
	local := strings.Replace(catalogueDefinition, "clear.", "precise.", 1)
	if err := SavePosition(root, "company", "graphic-designer", local); err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(root).PositionText("graphic-designer"); got != local {
		t.Fatal("company override lost")
	}
	if got, _ := PositionSet(root, "global").PositionText("graphic-designer"); got != catalogueDefinition {
		t.Fatal("global definition changed")
	}
	before, err := Load(root).RoleDocAs("graphic-designer-1", "graphic-designer", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := DeletePosition(root, "company", "graphic-designer"); err != nil {
		t.Fatal(err)
	}
	for _, p := range Load(root).Positions() {
		if p.Name == "graphic-designer" {
			t.Fatal("deleted profession still offered for hiring")
		}
	}
	after, err := Load(root).RoleDocAs("graphic-designer-1", "graphic-designer", "")
	if err != nil || after != before {
		t.Fatal("deletion changed existing employee")
	}
	if err := Hire(root, config.Default(), "graphic-designer-2", "graphic-designer", "", false); err == nil {
		t.Fatal("hired deleted profession")
	}
	if err := RestorePosition(root, "company", "graphic-designer"); err != nil {
		t.Fatal(err)
	}
	if Load(root).Archetype("graphic-designer-2") != "graphic-designer" {
		t.Fatal("restore did not restore hiring")
	}
	if err := DeletePosition(root, "company", "developer"); err != nil {
		t.Fatal(err)
	}
	if Load(root).Archetype("developer") != "" {
		t.Fatal("deleted built-in reappeared")
	}
	if _, err := Load(root).PositionText("developer"); err != nil {
		t.Fatal("deleted built-in lost definition")
	}
}
func TestCatalogueRejectsInvalidDefinitions(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	for _, name := range []string{"../escape", "", "Bad Name", "x/y"} {
		if SavePosition(root, "company", name, catalogueDefinition) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, text := range []string{"", "Title: Empty\nSector: universal\n\n## Your remit\n\n## Your bias\n", strings.ReplaceAll(catalogueDefinition, "creative-media", "absent-pool"), strings.ReplaceAll(catalogueDefinition, "## Your bias", "## Other")} {
		if SavePosition(root, "company", "example", text) == nil {
			t.Fatal("accepted invalid definition")
		}
	}
	if _, err := os.Stat(filepath.Join(config.LocalDir(root), "templates")); !os.IsNotExist(err) {
		t.Fatal("invalid write created templates")
	}
}
func TestGeneratePositionReturnsDraftWithoutInstalling(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	dir := t.TempDir()
	script := filepath.Join(dir, "generator")
	prompt := filepath.Join(dir, "prompt")
	text := "#!/bin/sh\ncat > '" + prompt + "'\ncat <<'EOF'\n" + catalogueDefinition + "EOF\n"
	if err := os.WriteFile(script, []byte(text), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.RoleGenerator.Command = []string{script}
	out, err := GeneratePosition(context.Background(), root, "generated-example", "Visual communication", cfg)
	if err != nil || out != catalogueDefinition {
		t.Fatalf("%q %v", out, err)
	}
	request, _ := os.ReadFile(prompt)
	for _, want := range []string{"Visual communication", "Title: Developer", "## Your remit", "Personal experience and temperament"} {
		if !strings.Contains(string(request), want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if _, err := Load(root).PositionText("generated-example"); err == nil {
		t.Fatal("generation installed draft")
	}
	cfg.RoleGenerator.Command = []string{"/bin/sleep", "20"}
	cfg.RoleGenerator.Timeout = time.Millisecond
	start := time.Now()
	if _, err := GeneratePosition(context.Background(), root, "example", "anything", cfg); err == nil || time.Since(start) > time.Second {
		t.Fatal("generation timeout failed")
	}
}
