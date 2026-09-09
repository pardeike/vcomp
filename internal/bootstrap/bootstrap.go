// Package bootstrap creates a company on disk and renders the documents the
// agents live by. No prose lives in this file: every word an agent reads comes
// from a template, resolved the same way settings are - the company's own
// .vcomp/templates/, then ~/.vcomp/templates/, then the built-in copies.
package bootstrap

import (
	"embed"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"vcomp/internal/config"
	"vcomp/internal/space"
)

//go:embed templates
var builtin embed.FS

// archetypes are the role templates that ship with vcomp. A role name is
// mapped onto one of these; anything unrecognised gets the generic template.
var archetypes = map[string]bool{
	"ceo": true, "project-master": true, "developer": true,
	"art-director": true, "tester": true, "hr": true,
}

var numberSuffix = regexp.MustCompile(`-\d+$`)

// Archetype maps a role name onto a template: "developer-2" -> "developer".
func Archetype(name string) string {
	base := numberSuffix.ReplaceAllString(name, "")
	if archetypes[base] {
		return base
	}
	return "generic"
}

// Set renders documents, looking through a chain of override directories
// before falling back to the templates compiled into the binary.
type Set struct{ dirs []string }

// Load returns the templates that apply to a company root.
func Load(root string) Set {
	return Set{dirs: []string{
		filepath.Join(config.LocalDir(root), config.TemplatesDir),
		filepath.Join(config.Home(), config.TemplatesDir),
	}}
}

// Builtin returns the shipped templates only.
func Builtin() Set { return Set{} }

func (s Set) read(name string) (string, error) {
	for _, dir := range s.dirs {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
			return string(b), nil
		}
	}
	b, err := builtin.ReadFile(path.Join("templates", name))
	if err != nil {
		return "", fmt.Errorf("no template %q", name)
	}
	return string(b), nil
}

// Text renders a template with the given {{KEY}} substitutions.
func (s Set) Text(name string, vars map[string]string) (string, error) {
	doc, err := s.read(name)
	if err != nil {
		return "", err
	}
	for k, v := range vars {
		doc = strings.ReplaceAll(doc, "{{"+k+"}}", strings.TrimSpace(v))
	}
	return doc, nil
}

// Backstory picks one of the archetype's backgrounds, deterministically from
// the role's name, so a given name always gets the same person.
func (s Set) Backstory(name string) string {
	text, err := s.read("backstories/" + Archetype(name) + ".txt")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	h := fnv.New32a()
	h.Write([]byte(name))
	return strings.TrimSpace(lines[int(h.Sum32())%len(lines)])
}

// RoleDoc renders the role.md for a new employee: the archetype's remit and
// bias, its backstory, and the standing behaviour every role shares.
func (s Set) RoleDoc(name string) (string, error) {
	standing, err := s.read("standing.md")
	if err != nil {
		return "", err
	}
	return s.Text("role_"+Archetype(name)+".md", map[string]string{
		"NAME":      name,
		"BACKSTORY": wrap(s.Backstory(name), 78),
		"STANDING":  standing,
	})
}

// wrap reflows a paragraph so an inserted backstory matches the prose around
// it, since these documents are meant to be read and edited by hand.
func wrap(text string, width int) string {
	var b strings.Builder
	line := 0
	for i, word := range strings.Fields(text) {
		switch {
		case i == 0:
		case line+1+len(word) > width:
			b.WriteString("\n")
			line = 0
		default:
			b.WriteString(" ")
			line++
		}
		b.WriteString(word)
		line += len(word)
	}
	return b.String()
}

// Export writes the built-in templates into dir, which is how the global
// defaults in ~/.vcomp/templates/ get installed. Existing files are only
// replaced when force is set.
func Export(dir string, force bool) (written []string, err error) {
	err = fs.WalkDir(builtin, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(p, "templates")))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Stat(target); err == nil && !force {
			return nil
		}
		b, err := builtin.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		written = append(written, target)
		return nil
	})
	return written, err
}

// Exists reports whether root already holds a company.
func Exists(root string) bool {
	_, err := os.Stat(filepath.Join(root, space.SpacesDir, "ceo", space.RoleFile))
	return err == nil
}

// Init lays out a company in root, using cfg for the roster and the goal. It
// writes no settings file: what to persist is the caller's decision.
func Init(root string, cfg config.Config) error {
	if Exists(root) {
		return fmt.Errorf("%s already holds a company", root)
	}
	if strings.TrimSpace(cfg.Goal) == "" {
		return fmt.Errorf("a goal is required (-goal, or 'goal =' in %s)", config.FileName)
	}
	hasCEO := false
	for _, name := range cfg.Roster {
		if name == "ceo" {
			hasCEO = true
		}
	}
	if !hasCEO {
		return fmt.Errorf("the roster must include a ceo, got %v", cfg.Roster)
	}

	for _, d := range []string{space.SpacesDir, space.PublicDir, space.ProductDir, config.DirName} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
	}
	set := Load(root)

	conventions, err := set.Text("CONVENTIONS.md", map[string]string{"RESULT": cfg.ResultFile})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "CONVENTIONS.md"), []byte(conventions), 0o644); err != nil {
		return err
	}

	for _, name := range cfg.Roster {
		dir := filepath.Join(root, space.SpacesDir, name)
		if err := os.MkdirAll(filepath.Join(dir, "inbox"), 0o755); err != nil {
			return err
		}
		doc, err := set.RoleDoc(name)
		if err != nil {
			return err
		}
		doc = strings.ReplaceAll(doc, "{{RESULT}}", cfg.ResultFile)
		if err := os.WriteFile(filepath.Join(dir, space.RoleFile), []byte(doc), 0o644); err != nil {
			return err
		}
	}

	goalDoc, err := set.Text("goal.md", map[string]string{
		"GOAL":   cfg.Goal,
		"RESULT": cfg.ResultFile,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, space.SpacesDir, "ceo", "goal.md"), []byte(goalDoc), 0o644); err != nil {
		return err
	}
	return initProduct(set, filepath.Join(root, space.ProductDir))
}

// EnsureLayout creates any missing infrastructure in a company that already
// exists - the standard directories, and an inbox for every role - without
// touching anything that is already there. It returns what it had to make.
func EnsureLayout(root string) ([]string, error) {
	dirs := []string{space.SpacesDir, space.PublicDir, space.ProductDir, config.DirName}
	roles, _ := space.Roles(root)
	for _, r := range roles {
		dirs = append(dirs, filepath.Join(space.SpacesDir, r.Name, "inbox"))
	}
	var made []string
	for _, d := range dirs {
		p := filepath.Join(root, d)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return made, err
		}
		made = append(made, d)
	}
	return made, nil
}

// Describe summarises what is actually on disk, so that "where did my company
// go" is answerable without a file manager.
func Describe(root string) string {
	roles, _ := space.Roles(root)
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, r.Name)
	}
	runs := len(space.Runs(root))
	return fmt.Sprintf("%s\n  spaces/   %d roles: %s\n  product/  the git repo they build in\n"+
		"  public/   %d user runs\n  %s/  settings and engine state",
		root, len(names), strings.Join(names, ", "), runs, config.DirName)
}

// SyncGoal rewrites the CEO's goal.md when the configured goal no longer
// matches it, so that changing "goal =" actually reaches the only person who is
// ever told it. Without this the goal is frozen at creation and a later edit is
// silently ignored.
func SyncGoal(root string, cfg config.Config) (bool, error) {
	goal := strings.TrimSpace(cfg.Goal)
	if goal == "" {
		return false, nil
	}
	p := filepath.Join(root, space.SpacesDir, "ceo", "goal.md")
	if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), goal) {
		return false, nil
	}
	doc, err := Load(root).Text("goal.md", map[string]string{
		"GOAL":   goal,
		"RESULT": cfg.ResultFile,
	})
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(p, []byte(doc), 0o644)
}

// initProduct makes the artifact a real git repo with one commit, so that
// "read the diff since last time" works from the very first tick.
func initProduct(set Set, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	readme, err := set.Text("product_readme.md", nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return err
	}
	git := func(args ...string) error {
		c := exec.Command("git", append([]string{"-C", dir,
			"-c", "user.name=vcomp", "-c", "user.email=vcomp@localhost"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := git("init", "-q", "-b", "main"); err != nil {
		return err
	}
	if err := git("add", "."); err != nil {
		return err
	}
	return git("commit", "-q", "-m", "empty product")
}
