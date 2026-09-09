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
		"ceo":             "ceo",
		"developer-1":     "developer",
		"developer-12":    "developer",
		"hr":              "hr",
		"art-director":    "art-director",
		"ux-designer":     "ux-designer",
		"ux-designer-2":   "ux-designer",
		"sound-designer":  "sound-designer",
		"finance-manager": "finance-manager",
		"chief-vibes-1":   "",
		"nonsense":        "",
	} {
		if got := Builtin().Archetype(name); got != want {
			t.Errorf("Archetype(%q) = %q, want %q", name, got, want)
		}
	}
}

// Every catalogue position must render into a complete role document, since a
// broken one is only discovered when someone puts it in a roster.
func TestEveryCataloguePositionRenders(t *testing.T) {
	set := Builtin()
	positions := set.Positions()
	if len(positions) < 20 {
		t.Fatalf("expected the full catalogue, got %d positions", len(positions))
	}
	for _, p := range positions {
		if p.Title == "" || p.Sector == "" {
			t.Errorf("%s: missing title or sector", p.Name)
		}
		doc, err := set.RoleDoc(p.Name)
		if err != nil {
			t.Errorf("%s: %v", p.Name, err)
			continue
		}
		if strings.Contains(expandCompany(doc, config.Default()), "{{") {
			t.Errorf("%s: unsubstituted placeholder", p.Name)
		}
		for _, heading := range []string{"## You are", "## Your remit", "## Your bias",
			"## How you think", "## When your inbox is empty"} {
			if !strings.Contains(doc, heading) {
				t.Errorf("%s: missing %q", p.Name, heading)
			}
		}
		if !strings.Contains(doc, p.Title) {
			t.Errorf("%s: the title %q never appears in the document", p.Name, p.Title)
		}
		// A backstory must have been drawn from the position's sector pool.
		if set.flavour(p.Sector, p.Name) == "" {
			t.Errorf("%s: no backstory pool for sector %q", p.Name, p.Sector)
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
	if cfg, err := config.Load(root); err != nil || cfg.Goal != "make something people want" {
		t.Fatalf("effective goal was not retained for reset: %q %v", cfg.Goal, err)
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

// A role's identity is a shared profession plus a personal flavour. Two people
// of the same type must read identically apart from the flavour, so that a
// company's idea of what a designer does cannot drift person by person.
func TestSameProfessionDiffersOnlyInFlavour(t *testing.T) {
	set := Builtin()
	a, err := set.RoleDoc("designer-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := set.RoleDoc("designer-2")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two designers should not be the same person")
	}
	// The professional half is the document with the "You are" section removed.
	profession := func(doc string) string {
		return doc[strings.Index(doc, "## Your remit"):]
	}
	if profession(a) != profession(b) {
		t.Error("two designers must share an identical professional description")
	}
	if set.flavour("universal", "designer-1") == set.flavour("universal", "designer-2") {
		t.Error("two designers should have different flavour")
	}

	// A background handed in at hiring time replaces the pool, and nothing else.
	custom, err := set.RoleDocAs("designer-3", "designer", "You once designed slot machines and it still bothers you.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(custom, "slot machines") {
		t.Error("a supplied backstory should be used")
	}
	if profession(custom) != profession(a) {
		t.Error("a supplied backstory must not change the professional description")
	}
}

func TestTemplatesResolveLocalThenGlobalThenBuiltIn(t *testing.T) {
	home := hermetic(t)
	root := t.TempDir()

	// A global template applies to every company.
	globalDir := filepath.Join(home, config.TemplatesDir, "positions")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	global := "Title: Developer\nSector: developer\n\n## Your remit\nYou work for the whole estate.\n"
	if err := os.WriteFile(filepath.Join(globalDir, "developer.md"), []byte(global), 0o644); err != nil {
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
	localDir := filepath.Join(config.LocalDir(root), config.TemplatesDir, "positions")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	local := "Title: Developer\nSector: developer\n\n## Your remit\nYou only write Fortran.\n"
	if err := os.WriteFile(filepath.Join(localDir, "developer.md"), []byte(local), 0o644); err != nil {
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

func TestRoleInputsPreserveProfession(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	cfg := settings("build a thing", "ceo")
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	if err := Hire(root, cfg, "alex", "developer", "You built games.", false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "spaces/alex/role.md"))
	if err := Steer(root, cfg, "alex", "Focus on keyboard use."); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "spaces/alex/role.md"))
	if !strings.HasPrefix(string(after), strings.TrimRight(string(before), "\n")) || !strings.Contains(string(after), "Focus on keyboard use.") {
		t.Fatal("steering replaced the fixed document")
	}
	if err := Hire(root, cfg, "alex", "tester", "new background", true); err == nil {
		t.Fatal("replacement changed profession")
	}
	if err := Hire(root, cfg, "unknown", "", "", false); err == nil {
		t.Fatal("unknown profession accepted")
	}
	if err := Hire(root, cfg, "ceo", "ceo", "replacement", true); err == nil {
		t.Fatal("CEO is user-configured")
	}
	if err := Steer(root, cfg, "ceo", "replacement"); err == nil {
		t.Fatal("CEO steering accepted")
	}
	path := filepath.Join(root, "spaces/alex/role.md")
	os.WriteFile(path, []byte("become someone else"), 0644)
	if _, err := RefreshRole(root, cfg, "alex"); err != nil {
		t.Fatal(err)
	}
	repaired, _ := os.ReadFile(path)
	if string(repaired) != string(after) {
		t.Fatal("generated profession was not restored")
	}
}

func TestCEOUserTweaksAndFixedTemplate(t *testing.T) {
	home := hermetic(t)
	root := t.TempDir()
	cfg := settings("build a thing", "ceo")
	dir := filepath.Join(home, "templates", "positions")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "ceo.md"), []byte("Title: Chief Executive\nSector: ceo\n\n## Your remit\nFixed user-owned CEO base.\n"), 0644)
	cfg.CEOInstructionsFile = "ceo-notes.md"
	os.WriteFile(filepath.Join(root, cfg.CEOInstructionsFile), []byte("Keep the team small.\nAsk for evidence."), 0644)
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	doc, _ := os.ReadFile(filepath.Join(root, "spaces/ceo/role.md"))
	if !strings.Contains(string(doc), "Fixed user-owned CEO base.") || !strings.HasSuffix(string(doc), "Keep the team small.\nAsk for evidence.\n") {
		t.Fatalf("CEO composition wrong: %s", doc)
	}
}

func TestResetValidatesBeforeDeletingAndRetainsFlagGoal(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	cfg := settings("line one\nline two", "ceo")
	if err := Init(root, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(root)
	if err != nil || loaded.Goal != cfg.Goal {
		t.Fatalf("goal lost: %q %v", loaded.Goal, err)
	}
	sentinel := filepath.Join(root, "product", "keep.txt")
	os.WriteFile(sentinel, []byte("work"), 0644)
	bad := loaded
	bad.Goal = ""
	if err := Reset(root, bad); err == nil {
		t.Fatal("invalid rebuild accepted")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal("work deleted before validation")
	}
	if err := Reset(root, loaded); err != nil {
		t.Fatal(err)
	}
	if !Exists(root) {
		t.Fatal("company was not rebuilt")
	}
}
