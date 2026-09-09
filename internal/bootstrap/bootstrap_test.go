package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vcomp/internal/config"
)

// hermetic points the global settings directory at an empty temp dir, so tests
// never pick up the developer's own ~/.vcomp.
func hermetic(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.HomeEnv, home)
	return home
}

func settings(goal string, roster ...string) config.Config {
	c := config.Default()
	c.Goal = goal
	if len(roster) > 0 {
		c.Roster = roster
	}
	return c
}

func TestArchetypeMapping(t *testing.T) {
	for name, want := range map[string]string{
		"ceo":           "ceo",
		"developer-1":   "developer",
		"developer-12":  "developer",
		"hr":            "hr",
		"art-director":  "art-director",
		"chief-vibes-1": "generic",
		"nonsense":      "generic",
	} {
		if got := Archetype(name); got != want {
			t.Errorf("Archetype(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestInitProducesAWorkingCompany(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	roster := []string{"ceo", "hr", "developer-1", "developer-2"}
	if err := Init(root, settings("make something people want", roster...)); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{
		"CONVENTIONS.md", "product/README.md", "product/.git",
		"spaces/ceo/goal.md", "spaces/ceo/inbox",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// Creating a company writes no settings file: an untouched company runs
	// entirely on the inherited defaults.
	if _, err := os.Stat(filepath.Join(config.LocalDir(root), config.FileName)); err == nil {
		t.Error("init should not write a settings file")
	}

	// Every role document must be fully rendered and carry both the archetype's
	// remit and the standing behaviour shared by everyone.
	for _, name := range roster {
		b, err := os.ReadFile(filepath.Join(root, "spaces", name, "role.md"))
		if err != nil {
			t.Fatal(err)
		}
		doc := string(b)
		if strings.Contains(doc, "{{") {
			t.Errorf("%s: unsubstituted placeholder in role.md", name)
		}
		for _, heading := range []string{"## You are", "## How you think", "## How you speak",
			"## When your inbox is empty", "## Your own goals"} {
			if strings.Count(doc, heading) != 1 {
				t.Errorf("%s: want exactly one %q, got %d", name, heading, strings.Count(doc, heading))
			}
		}
	}

	// The goal is the CEO's alone, and the CEO is told how to end the company.
	goal, err := os.ReadFile(filepath.Join(root, "spaces", "ceo", "goal.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goal), "make something people want") {
		t.Error("the goal did not reach the CEO")
	}
	ceo, _ := os.ReadFile(filepath.Join(root, "spaces", "ceo", "role.md"))
	if !strings.Contains(string(ceo), config.Default().ResultFile) {
		t.Error("the CEO was not told which file ends the simulation")
	}
	for _, name := range []string{"hr", "developer-1"} {
		if _, err := os.Stat(filepath.Join(root, "spaces", name, "goal.md")); err == nil {
			t.Errorf("%s should not have been given the goal", name)
		}
	}

	// Two people sharing an archetype must not be the same person.
	one, _ := os.ReadFile(filepath.Join(root, "spaces", "developer-1", "role.md"))
	two, _ := os.ReadFile(filepath.Join(root, "spaces", "developer-2", "role.md"))
	if string(one) == string(two) {
		t.Error("the two developers got identical backstories")
	}

	if err := Init(root, settings("again")); err == nil {
		t.Error("init should refuse to populate an existing company")
	}
}

func TestInitRejectsIncompleteSettings(t *testing.T) {
	hermetic(t)
	if err := Init(t.TempDir(), settings("")); err == nil {
		t.Error("a company without a goal should be rejected")
	}
	if err := Init(t.TempDir(), settings("goal", "developer-1")); err == nil {
		t.Error("a company without a ceo should be rejected")
	}
}

// Changing the goal after the company exists has to reach the CEO, who is the
// only one ever told it. Before this, an edited goal was silently ignored.
func TestSyncGoalReachesTheCEOAfterTheFact(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	cfg := settings("build a tiny maze game")
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	goalPath := filepath.Join(root, "spaces", "ceo", "goal.md")

	// An unchanged goal must not touch the file.
	if changed, err := SyncGoal(root, cfg); err != nil || changed {
		t.Fatalf("SyncGoal on an unchanged goal = %v, %v; want false, nil", changed, err)
	}

	cfg.Goal = "build a classical text adventure with a memorable ending"
	changed, err := SyncGoal(root, cfg)
	if err != nil || !changed {
		t.Fatalf("SyncGoal on a changed goal = %v, %v; want true, nil", changed, err)
	}
	b, err := os.ReadFile(goalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "memorable ending") {
		t.Errorf("the new goal did not reach the CEO:\n%s", b)
	}
	if strings.Contains(string(b), "maze") {
		t.Errorf("the old goal is still there:\n%s", b)
	}
	// And it is still a rendered document, not a bare line.
	if strings.Contains(string(b), "{{") || !strings.Contains(string(b), "RESULT.md") {
		t.Errorf("goal.md was not rendered from the template:\n%s", b)
	}
}

// Reset is "start this run over", not "start from nothing": everything the
// company produced goes, everything that was configured stays.
func TestResetClearsWorkButKeepsSettings(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	cfg := settings("build a thing", "ceo", "developer-1")
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}

	// Settings and a local template override: both must survive.
	local := config.LocalDir(root)
	if err := os.MkdirAll(filepath.Join(local, config.TemplatesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(local, config.FileName)
	if err := os.WriteFile(conf, []byte("goal = build a thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl := filepath.Join(local, config.TemplatesDir, "standing.md")
	if err := os.WriteFile(tmpl, []byte("my own standing orders"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Work the company produced.
	produced := map[string]string{
		filepath.Join(root, "spaces", "developer-1", "inbox", "a-task", "message.md"): "please do this",
		filepath.Join(root, "spaces", "ceo", "notes.md"):                              "my private thoughts",
		filepath.Join(root, "product", "MADE.md"):                                     "the artifact",
		filepath.Join(root, "public", "run-0001", "impressions.md"):                   "it was fine",
		filepath.Join(root, cfg.ResultFile):                                           "we are done",
	}
	for p, body := range produced {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := Reset(root, cfg); err != nil {
		t.Fatal(err)
	}

	for p := range produced {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived the reset", p)
		}
	}
	for _, p := range []string{conf, tmpl} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("reset destroyed a setting it should have kept: %s", p)
		}
	}
	// And it is a working company again, not just an empty directory.
	if !Exists(root) {
		t.Fatal("reset should have rebuilt the company")
	}
	for _, p := range []string{"CONVENTIONS.md", "product/README.md", "product/.git",
		"spaces/ceo/goal.md", "spaces/developer-1/inbox"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("rebuilt company is missing %s", p)
		}
	}
	// The rebuild uses the kept templates.
	doc, err := os.ReadFile(filepath.Join(root, "spaces", "developer-1", "role.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "my own standing orders") {
		t.Error("the rebuild ignored the local template that reset kept")
	}
}

func TestTemplatesResolveLocalThenGlobalThenBuiltIn(t *testing.T) {
	home := hermetic(t)
	root := t.TempDir()

	// A global template applies to every company.
	globalDir := filepath.Join(home, config.TemplatesDir)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	global := "# {{NAME}}\n\n{{BACKSTORY}}\n\nYou work for the whole estate.\n\n{{STANDING}}\n"
	if err := os.WriteFile(filepath.Join(globalDir, "role_developer.md"), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(root).RoleDoc("developer-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "You work for the whole estate.") {
		t.Fatalf("global template was ignored:\n%s", doc)
	}

	// The company's own copy wins over the global one.
	localDir := filepath.Join(config.LocalDir(root), config.TemplatesDir)
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	local := "# {{NAME}}\n\n{{BACKSTORY}}\n\nYou only write Fortran.\n\n{{STANDING}}\n"
	if err := os.WriteFile(filepath.Join(localDir, "role_developer.md"), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err = Load(root).RoleDoc("developer-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "You only write Fortran.") {
		t.Fatalf("company template should beat the global one:\n%s", doc)
	}
	if strings.Contains(doc, "{{") || !strings.Contains(doc, "## How you think") {
		t.Fatalf("an override still has to be rendered in full:\n%s", doc)
	}

	// Anything not overridden still comes from the built-in set.
	if doc, err := Load(root).RoleDoc("ceo"); err != nil || !strings.Contains(doc, "Chief Executive") {
		t.Fatalf("unmodified templates should still work, got %v", err)
	}
}

func TestExportInstallsAndProtectsEdits(t *testing.T) {
	dir := t.TempDir()
	written, err := Export(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) < 10 {
		t.Fatalf("expected the whole template set, got %d files", len(written))
	}
	edited := filepath.Join(dir, "standing.md")
	if err := os.WriteFile(edited, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A second install leaves existing files alone...
	again, err := Export(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Errorf("re-installing should have written nothing, wrote %v", again)
	}
	if b, _ := os.ReadFile(edited); string(b) != "mine" {
		t.Error("re-installing overwrote an edited template")
	}

	// ...unless it is forced.
	if _, err := Export(dir, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(edited); string(b) == "mine" {
		t.Error("a forced install should have replaced the template")
	}
}
