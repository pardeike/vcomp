package main

import (
	"os"
	"path/filepath"
	"testing"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
)

func TestProfessionCommands(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	file := filepath.Join(t.TempDir(), "definition.md")
	text := "Title: Print Designer\nSector: creative-media\n\n## Your remit\nMake legible print layouts.\n\n## Your bias\nPrefer evidence on paper.\n"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(operation string) error {
		return cmdProfession([]string{operation, "print-designer", "-root", root, "-file", file, "-scope", "global"})
	}
	if err := run("create"); err != nil {
		t.Fatal(err)
	}
	if err := run("create"); err == nil {
		t.Fatal("create overwrote existing definition")
	}
	if err := run("update"); err != nil {
		t.Fatal(err)
	}
	if err := run("delete"); err != nil {
		t.Fatal(err)
	}
	if bootstrap.Load(root).Archetype("print-designer") != "" {
		t.Fatal("deleted profession remains active")
	}
	if err := run("restore"); err != nil {
		t.Fatal(err)
	}
	if bootstrap.Load(root).Archetype("print-designer") != "print-designer" {
		t.Fatal("restore failed")
	}
}
