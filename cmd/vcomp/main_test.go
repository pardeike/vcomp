package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/tmux"
)

func TestEngineChild(t *testing.T) {
	if os.Getenv("VCOMP_ENGINE_CHILD") == "" {
		return
	}
	if err := cmdRun([]string{"-root", os.Getenv("VCOMP_ENGINE_CHILD"), "-goal", "flag goal\nretained on reset"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func isolated(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	bin := t.TempDir()
	socket := fmt.Sprintf("vcomp-cli-test-%d", os.Getpid())
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexec "+real+" -L "+socket+" \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(config.HomeEnv, t.TempDir())
}

func startChild(t *testing.T, root, prefix string) *exec.Cmd {
	t.Helper()
	if err := os.MkdirAll(config.LocalDir(root), 0755); err != nil {
		t.Fatal(err)
	}
	conf := "roster = ceo\nharness = fake\ntick = 20ms\nsession_prefix = " + prefix + "\n[harness fake]\nready_pattern = (?s).*\nstart = /bin/cat\nresume = /bin/cat\n"
	if err := os.WriteFile(filepath.Join(config.LocalDir(root), config.FileName), []byte(conf), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestEngineChild$")
	cmd.Env = append(os.Environ(), "VCOMP_ENGINE_CHILD="+root)
	log, err := os.Create(filepath.Join(root, "child.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait(); tmux.Kill(prefix + "-ceo"); log.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for !tmux.Alive(prefix + "-ceo") {
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(log.Name())
			t.Fatalf("child did not start: %s", b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cmd
}

func TestStopAndResetRunningCompany(t *testing.T) {
	isolated(t)
	root := t.TempDir()
	child := startChild(t, root, "cli-stop")
	if err := cmdStop([]string{"-root", root}); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if tmux.Exists("cli-stop-ceo") {
		t.Fatal("stop left an agent")
	}
	if err := cmdReset([]string{"-root", root, "-y"}); err != nil {
		t.Fatal(err)
	}
	if !bootstrap.Exists(root) {
		t.Fatal("reset failed to rebuild a flag-goal company")
	}
	cfg, err := config.Load(root)
	if err != nil || cfg.Goal != "flag goal\nretained on reset" {
		t.Fatalf("goal lost: %q %v", cfg.Goal, err)
	}
	root = t.TempDir()
	child = startChild(t, root, "cli-reset")
	if err := cmdReset([]string{"-root", root, "-y"}); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if tmux.Exists("cli-reset-ceo") || !bootstrap.Exists(root) {
		t.Fatal("reset raced with the old engine")
	}
}

func TestStopAllFindsCustomPrefixes(t *testing.T) {
	isolated(t)
	a := startChild(t, t.TempDir(), "alpha")
	b := startChild(t, t.TempDir(), "beta")
	if err := cmdStop([]string{"-all"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := b.Wait(); err != nil {
		t.Fatal(err)
	}
	if tmux.Exists("alpha-ceo") || tmux.Exists("beta-ceo") {
		t.Fatal("stop-all missed custom prefixes")
	}
}

func TestSetupKeepsAcceptedAnswers(t *testing.T) {
	t.Setenv(config.HomeEnv, t.TempDir())
	root := t.TempDir()
	os.MkdirAll(config.LocalDir(root), 0755)
	os.WriteFile(filepath.Join(config.LocalDir(root), config.FileName), []byte("goal = retained\nroster = ceo\nsession_prefix = custom\n[role ceo]\nmodel = custom-model\n"), 0644)
	input, err := os.CreateTemp(t.TempDir(), "answers")
	if err != nil {
		t.Fatal(err)
	}
	answers := []string{"", "", "", "", "", "2s", "", "", "", "", "", "", ""}
	input.WriteString(strings.Join(answers, "\n") + "\n")
	input.Seek(0, 0)
	old := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = old; input.Close() }()
	if err := setupCompany(root); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Goal != "retained" || cfg.SessionPrefix != "custom" || cfg.Roles["ceo"].Model != "custom-model" || cfg.Tick != 2*time.Second {
		t.Fatalf("setup lost settings: %+v", cfg)
	}
}

func TestStopWorksWithBrokenConfiguration(t *testing.T) {
	isolated(t)
	root := t.TempDir()
	child := startChild(t, root, "broken-config")
	os.WriteFile(filepath.Join(config.LocalDir(root), config.FileName), []byte("unknown_key = typo\n"), 0644)
	if err := cmdStop([]string{"-root", root}); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if tmux.Exists("broken-config-ceo") {
		t.Fatal("broken config prevented shutdown")
	}
}
