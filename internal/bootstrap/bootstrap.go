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
	"sort"
	"strings"

	"vcomp/internal/config"
	"vcomp/internal/space"
)

//go:embed templates
var builtin embed.FS

var numberSuffix = regexp.MustCompile(`-\d+$`)

// PositionsDir holds the catalogue of shorter role definitions: a title, a
// sector, a remit and a bias, rendered through the role_position.md frame.
const PositionsDir = "positions"

// Position is one entry in that catalogue.
type Position struct {
	Name   string
	Title  string
	Sector string
}

// Set renders documents, looking through a chain of override directories
// before falling back to the templates compiled into the binary.
type Set struct{ dirs []string }

// Archetype maps a role name onto whatever template can describe it:
// "developer-2" -> "developer", "ux-designer-1" -> "ux-designer". The set of
// roles is therefore whatever templates exist, not a list in this file.
// Anything unrecognised gets the generic template.
func (s Set) Archetype(name string) string {
	base := numberSuffix.ReplaceAllString(name, "")
	if _, err := s.read("role_" + base + ".md"); err == nil {
		return base
	}
	if _, err := s.read(path.Join(PositionsDir, base+".md")); err == nil {
		return base
	}
	return "generic"
}

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

// Backstory picks one of a pool's backgrounds, deterministically from the
// role's name, so a given name always gets the same person.
func (s Set) Backstory(name string) string {
	return s.backstoryFrom(s.Archetype(name), name)
}

func (s Set) backstoryFrom(pool, name string) string {
	text, err := s.read(path.Join("backstories", pool+".txt"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	h := fnv.New32a()
	h.Write([]byte(name))
	return strings.TrimSpace(lines[int(h.Sum32())%len(lines)])
}

// RoleDoc renders the role.md for a new employee. A role with its own template
// uses it; a catalogue position is rendered through the shared frame, with a
// backstory drawn from its sector.
func (s Set) RoleDoc(name string) (string, error) {
	standing, err := s.read("standing.md")
	if err != nil {
		return "", err
	}
	arch := s.Archetype(name)

	if body, err := s.read(path.Join(PositionsDir, arch+".md")); err == nil {
		title, sector, remit := splitPosition(body)
		return s.Text("role_position.md", map[string]string{
			"NAME":      name,
			"TITLE":     title,
			"BACKSTORY": wrap(s.backstoryFrom(sector, name), 78),
			"REMIT":     remit,
			"STANDING":  standing,
		})
	}
	return s.Text("role_"+arch+".md", map[string]string{
		"NAME":      name,
		"BACKSTORY": wrap(s.Backstory(name), 78),
		"STANDING":  standing,
	})
}

// splitPosition separates a position's "Title:" and "Sector:" header lines
// from the markdown body that follows them.
func splitPosition(body string) (title, sector, remit string) {
	lines := strings.Split(body, "\n")
	i := 0
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			break
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "title":
			title = strings.TrimSpace(value)
		case "sector":
			sector = strings.TrimSpace(value)
		default:
			return title, sector, strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return title, sector, strings.TrimSpace(strings.Join(lines[i:], "\n"))
}

// Positions lists the role catalogue, sorted by sector then name.
func (s Set) Positions() []Position {
	names := map[string]bool{}
	if entries, err := fs.ReadDir(builtin, path.Join("templates", PositionsDir)); err == nil {
		for _, e := range entries {
			names[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	for _, dir := range s.dirs {
		entries, err := os.ReadDir(filepath.Join(dir, PositionsDir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			names[strings.TrimSuffix(e.Name(), ".md")] = true
		}
	}
	var out []Position
	for name := range names {
		body, err := s.read(path.Join(PositionsDir, name+".md"))
		if err != nil {
			continue
		}
		title, sector, _ := splitPosition(body)
		out = append(out, Position{Name: name, Title: title, Sector: sector})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sector != out[j].Sector {
			return out[i].Sector < out[j].Sector
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// CoreRoles lists the roles that have a full hand-written template of their
// own, as opposed to a catalogue entry.
func (s Set) CoreRoles() []string {
	var names []string
	entries, err := fs.ReadDir(builtin, "templates")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := strings.TrimPrefix(strings.TrimSuffix(e.Name(), ".md"), "role_")
		if !strings.HasPrefix(e.Name(), "role_") || name == "position" || name == "generic" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

// Produced lists everything a company generated, as opposed to what was
// configured: the spaces and their contents, the artifact, the user runs, the
// rendered conventions, the result, and the engine's own bookkeeping. The
// settings and any local templates are deliberately not in here.
func Produced(root string, cfg config.Config) []string {
	paths := []string{
		filepath.Join(root, space.SpacesDir),
		filepath.Join(root, space.PublicDir),
		filepath.Join(root, space.ProductDir),
		filepath.Join(root, "CONVENTIONS.md"),
		filepath.Join(config.LocalDir(root), "state.json"),
		filepath.Join(config.LocalDir(root), "engine.log"),
	}
	if cfg.ResultFile != "" {
		paths = append(paths, filepath.Join(root, cfg.ResultFile))
	}
	var present []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			present = append(present, p)
		}
	}
	return present
}

// Reset removes what a company produced and builds it again from the same
// settings, so a run can be started over without re-entering anything. It
// removes only the known paths, never the directory it was given.
func Reset(root string, cfg config.Config) error {
	for _, p := range Produced(root, cfg) {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return Init(root, cfg)
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
