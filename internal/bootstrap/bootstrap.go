// Package bootstrap creates a company on disk and renders the documents the
// agents live by. No prose lives in this file: every word an agent reads comes
// from a template, resolved the same way settings are - the company's own
// .vcomp/templates/, then ~/.vcomp/templates/, then the built-in copies.
package bootstrap

import (
	"embed"
	"encoding/json"
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
// Unrecognised names need an explicit catalogue position.
func (s Set) Archetype(name string) string {
	base := numberSuffix.ReplaceAllString(name, "")
	if _, err := s.read(path.Join(PositionsDir, base+".md")); err == nil {
		return base
	}
	return ""
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

// A role's identity is two separable things: the profession, which is shared by
// everyone of that type and is identical every time, and the flavour, which
// varies per person. Only the second is allowed to vary, which is what keeps
// roles invented at runtime from drifting away from how roles should work.

// Backstory is the flavour half: a background plus one personality trait,
// chosen deterministically from the role's name, so a given name is always the
// same person and two designers are reliably different ones.
func (s Set) Backstory(name string) string {
	body, err := s.read(path.Join(PositionsDir, s.Archetype(name)+".md"))
	if err != nil {
		return ""
	}
	_, sector, _ := splitPosition(body)
	return s.flavour(sector, name)
}

func (s Set) flavour(pool, name string) string {
	background := pick(s.lines(path.Join("backstories", pool+".txt")), name)
	trait := pick(s.lines("traits.md"), name+"/trait")
	return strings.TrimSpace(background + " " + trait)
}

func (s Set) lines(file string) []string {
	text, err := s.read(file)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// pick chooses deterministically, so the same name always gets the same person.
func pick(options []string, seed string) string {
	if len(options) == 0 {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(seed))
	return options[int(h.Sum32())%len(options)]
}

// RoleDoc renders a catalogue profession with a backstory from its sector.
func (s Set) RoleDoc(name string) (string, error) {
	return s.RoleDocAs(name, "", "")
}

// RoleDocAs renders a role.md, optionally forcing which profession the person
// holds and what their background is. The remit and the standing behaviour
// still come from the shared templates either way.
func (s Set) RoleDocAs(name, position, backstory string) (string, error) {
	standing, err := s.read("standing.md")
	if err != nil {
		return "", err
	}
	if position == "" {
		position = s.Archetype(name)
	}
	if position == "" {
		return "", fmt.Errorf("%q needs a catalogue position (vcomp roles)", name)
	}
	body, err := s.read(path.Join(PositionsDir, position+".md"))
	if err != nil {
		return "", err
	}
	title, sector, remit := splitPosition(body)
	if title == "" || sector == "" || remit == "" {
		return "", fmt.Errorf("incomplete position %q", position)
	}
	if backstory == "" {
		backstory = s.flavour(sector, name)
	}
	return s.Text("role_position.md", map[string]string{
		"NAME": name, "TITLE": title, "BACKSTORY": wrap(backstory, 78),
		"REMIT": remit, "STANDING": standing, "STEERING": "",
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
	if _, err := os.Stat(filepath.Join(root, space.SpacesDir, "ceo", "role.json")); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(root, space.SpacesDir, "ceo", space.RoleFile))
	return err == nil
}

// Init lays out a company and retains a supplied goal for later resets.
func Init(root string, cfg config.Config) error {
	if Exists(root) {
		return fmt.Errorf("%s already holds a company", root)
	}
	if err := Validate(root, cfg); err != nil {
		return err
	}

	for _, d := range []string{space.SpacesDir, space.PublicDir, space.ProductDir, config.DirName} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
	}
	set := Load(root)

	conventions, err := set.Text("CONVENTIONS.md", map[string]string{
		"RESULT": cfg.ResultFile,
		"STATE":  cfg.StateFile,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "CONVENTIONS.md"), []byte(conventions), 0o644); err != nil {
		return err
	}

	// Retain a flag-supplied (possibly multiline) goal so reset and restart use it.
	current, err := config.Load(root)
	if err != nil {
		return err
	}
	if current.Goal != cfg.Goal {
		goalFile := filepath.Join(config.DirName, "goal.txt")
		if err := space.WriteFile(filepath.Join(root, goalFile), []byte(cfg.Goal), 0644); err != nil {
			return err
		}
		if err := config.UpdateLocal(root, []config.Override{{Key: "goal_file", Value: goalFile}}); err != nil {
			return err
		}
	}
	for _, name := range cfg.Roster {
		position := set.Archetype(name)
		if err := writeRole(root, cfg, name, RoleSpec{Position: position}); err != nil {
			return err
		}
	}

	goalDoc, err := set.Text("goal.md", map[string]string{
		"GOAL":   cfg.Goal,
		"RESULT": cfg.ResultFile,
		"STATE":  cfg.StateFile,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, space.SpacesDir, "ceo", "goal.md"), []byte(goalDoc), 0o644); err != nil {
		return err
	}
	return initProduct(set, filepath.Join(root, space.ProductDir))
}

// Hire writes a new role into a company from its two parts: a profession from
// the shared catalogue and a personal background. Composing it here rather than
// letting the document be written freehand is what stops a company's idea of
// what a role is from drifting as it invents new ones.
// RoleSpec is the editable input; role.md is always rendered from the catalogue.
// Authority is a company convention: HR supplies backstories, the CEO steering,
// and the user alone changes templates or CEO instructions.
type RoleSpec struct {
	Position  string `json:"position"`
	Backstory string `json:"backstory,omitempty"`
	Steering  string `json:"steering,omitempty"`
}

func readRole(root, name string) (RoleSpec, error) {
	var spec RoleSpec
	b, err := os.ReadFile(filepath.Join(root, space.SpacesDir, name, "role.json"))
	if err != nil {
		return spec, err
	}
	err = json.Unmarshal(b, &spec)
	return spec, err
}

func renderRole(root string, cfg config.Config, name string, spec RoleSpec) (string, error) {
	if name == "ceo" {
		spec = RoleSpec{Position: "ceo"}
	} else if spec.Position == "ceo" {
		return "", fmt.Errorf("the CEO position belongs only to ceo")
	}
	doc, err := Load(root).RoleDocAs(name, spec.Position, spec.Backstory)
	if err != nil {
		return "", err
	}
	steering := ""
	if name == "ceo" && cfg.CEOInstructionsFile != "" {
		p := cfg.CEOInstructionsFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		steering, err = Load(root).Text("ceo_tweaks.md", map[string]string{"TEXT": string(b)})
		if err != nil {
			return "", err
		}
	} else if name != "ceo" && spec.Steering != "" {
		steering, err = Load(root).Text("role_steering.md", map[string]string{"TEXT": spec.Steering})
		if err != nil {
			return "", err
		}
	}
	return expandCompany(strings.TrimRight(doc, "\n")+"\n\n"+steering, cfg), nil
}

func writeRole(root string, cfg config.Config, name string, spec RoleSpec) error {
	doc, err := renderRole(root, cfg, name, spec)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, space.SpacesDir, name)
	if err := os.MkdirAll(filepath.Join(dir, "inbox"), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	if err := space.WriteFile(filepath.Join(dir, "role.json"), b, 0644); err != nil {
		return err
	}
	return space.WriteFile(filepath.Join(dir, space.RoleFile), []byte(doc), 0644)
}

func Hire(root string, cfg config.Config, name, position, backstory string, replace bool) error {
	if err := RoleName(name); err != nil {
		return err
	}
	if name == "ceo" {
		return fmt.Errorf("the CEO is configured by the user, not hired or replaced")
	}
	spec, err := readRole(root, name)
	if err == nil {
		if !replace {
			return fmt.Errorf("%s already exists; -replace replaces its backstory", name)
		}
		if position != "" && position != spec.Position {
			return fmt.Errorf("%s keeps its fixed profession %s", name, spec.Position)
		}
		spec.Backstory = backstory
	} else {
		if !os.IsNotExist(err) {
			return err
		}
		if position == "" {
			position = Load(root).Archetype(name)
		}
		if position == "" {
			return fmt.Errorf("%q needs -position from vcomp roles", name)
		}
		spec = RoleSpec{Position: position, Backstory: backstory}
	}
	return writeRole(root, cfg, name, spec)
}

func RoleName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\:. \t\n") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "user-run-") {
		return fmt.Errorf("%q is not a usable role name", name)
	}
	return nil
}

// Steer changes only the final section, keeping the profession and backstory.
func Steer(root string, cfg config.Config, name, text string) error {
	if err := RoleName(name); err != nil {
		return err
	}
	if name == "ceo" {
		return fmt.Errorf("use the user setting ceo_instructions_file to steer the CEO")
	}
	spec, err := readRole(root, name)
	if err != nil {
		return err
	}
	spec.Steering = text
	return writeRole(root, cfg, name, spec)
}

// RefreshRole repairs edits to the generated document and applies edited inputs.
func RefreshRole(root string, cfg config.Config, name string) (space.Role, error) {
	spec, err := readRole(root, name)
	if err != nil {
		return space.Role{}, err
	}
	doc, err := renderRole(root, cfg, name, spec)
	if err != nil {
		return space.Role{}, err
	}
	dir := filepath.Join(root, space.SpacesDir, name)
	p := filepath.Join(dir, space.RoleFile)
	old, _ := os.ReadFile(p)
	if string(old) != doc {
		if err := space.WriteFile(p, []byte(doc), 0644); err != nil {
			return space.Role{}, err
		}
	}
	return space.Role{Name: name, Dir: dir, Hash: space.Hash([]byte(doc))}, nil
}

// Validate checks rebuild inputs before reset removes any company output.
func Validate(root string, cfg config.Config) error {
	if strings.TrimSpace(cfg.Goal) == "" {
		return fmt.Errorf("a goal is required before creating or resetting a company")
	}
	seen := map[string]bool{}
	for _, name := range cfg.Roster {
		if err := RoleName(name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("duplicate role %q", name)
		}
		seen[name] = true
		if _, err := renderRole(root, cfg, name, RoleSpec{Position: Load(root).Archetype(name)}); err != nil {
			return err
		}
	}
	if !seen["ceo"] {
		return fmt.Errorf("the roster must include ceo")
	}
	for _, name := range []string{"CONVENTIONS.md", "goal.md", "product_readme.md"} {
		if _, err := Load(root).read(name); err != nil {
			return err
		}
	}
	return nil
}

// expandCompany fills in the placeholders that depend on this company's
// settings rather than on the role.
func expandCompany(doc string, cfg config.Config) string {
	doc = strings.ReplaceAll(doc, "{{RESULT}}", cfg.ResultFile)
	return strings.ReplaceAll(doc, "{{STATE}}", cfg.StateFile)
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
	for _, f := range []string{cfg.ResultFile, cfg.StateFile, cfg.AuditFile} {
		if f != "" {
			paths = append(paths, filepath.Join(root, f))
		}
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
	if err := Validate(root, cfg); err != nil {
		return err
	}
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
	doc, err := Load(root).Text("goal.md", map[string]string{
		"GOAL":   goal,
		"RESULT": cfg.ResultFile,
		"STATE":  cfg.StateFile,
	})
	if err != nil {
		return false, err
	}
	if b, err := os.ReadFile(p); err == nil && string(b) == doc {
		return false, nil
	}
	return true, space.WriteFile(p, []byte(doc), 0o644)
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
