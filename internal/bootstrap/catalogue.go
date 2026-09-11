package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"vcomp/internal/config"
	"vcomp/internal/space"
)

func PositionName(name string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).MatchString(name) {
		return fmt.Errorf("profession name must be a lowercase slug, such as sound-designer")
	}
	return nil
}
func TemplateDir(root, scope string) (string, error) {
	switch scope {
	case "company":
		return filepath.Join(config.LocalDir(root), config.TemplatesDir), nil
	case "global":
		return filepath.Join(config.Home(), config.TemplatesDir), nil
	default:
		return "", fmt.Errorf("scope must be company or global")
	}
}
func (s Set) positionSource(name string) string {
	for _, dir := range s.dirs {
		p := filepath.Join(dir, PositionsDir, name+".md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "built-in"
}
func (s Set) positionDeleted(name string) bool {
	for _, dir := range s.dirs {
		if _, err := os.Stat(filepath.Join(dir, PositionsDir, name+".deleted")); err == nil {
			return true
		}
		if _, err := os.Stat(filepath.Join(dir, PositionsDir, name+".md")); err == nil {
			return false
		}
	}
	return false
}
func (s Set) PositionText(name string) (string, error) {
	if err := PositionName(name); err != nil {
		return "", err
	}
	return s.read(filepath.Join(PositionsDir, name+".md"))
}
func (s Set) ValidatePosition(name, text string) error {
	if err := PositionName(name); err != nil {
		return err
	}
	title, sector, body := splitPosition(text)
	if title == "" || sector == "" {
		return fmt.Errorf("definition needs Title: and Sector: headers")
	}
	if err := PositionName(sector); err != nil {
		return fmt.Errorf("invalid backstory sector: %w", err)
	}
	if len(s.lines("backstories/"+sector+".txt")) == 0 {
		return fmt.Errorf("no backstory pool for sector %q", sector)
	}
	for _, heading := range []string{"## Your remit", "## Your bias"} {
		found, content := false, false
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line == heading {
				found = true
				continue
			}
			if found && strings.HasPrefix(line, "#") {
				break
			}
			if found && line != "" {
				content = true
			}
		}
		if !content {
			return fmt.Errorf("definition needs a nonempty %s section", heading)
		}
	}
	return nil
}
func PositionSet(root, scope string) Set {
	if scope == "global" {
		return Set{dirs: []string{filepath.Join(config.Home(), config.TemplatesDir)}}
	}
	return Load(root)
}
func SavePosition(root, scope, name, text string) error {
	dir, err := TemplateDir(root, scope)
	if err != nil {
		return err
	}
	if err = PositionSet(root, scope).ValidatePosition(name, text); err != nil {
		return err
	}
	dir = filepath.Join(dir, PositionsDir)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err = space.WriteFile(filepath.Join(dir, name+".md"), []byte(strings.TrimSpace(text)+"\n"), 0644); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(dir, name+".deleted")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Deletion removes a profession from hiring, retaining its definition for
// existing employees. A scope-local copy prevents lower-layer changes from
// silently changing a deleted profession's surviving employees.
func DeletePosition(root, scope, name string) error {
	dir, err := TemplateDir(root, scope)
	if err != nil {
		return err
	}
	text, err := PositionSet(root, scope).PositionText(name)
	if err != nil {
		return err
	}
	if err = SavePosition(root, scope, name, text); err != nil {
		return err
	}
	return space.WriteFile(filepath.Join(dir, PositionsDir, name+".deleted"), []byte("Deleted from catalogue; definition retained for existing employees.\n"), 0644)
}
func RestorePosition(root, scope, name string) error {
	text, err := PositionSet(root, scope).PositionText(name)
	if err != nil {
		return err
	}
	return SavePosition(root, scope, name, text)
}

// GeneratePosition returns a draft only. No profession is installed by generation.
func GeneratePosition(ctx context.Context, root, name, brief string, cfg config.Config) (string, error) {
	if err := PositionName(name); err != nil {
		return "", err
	}
	if strings.TrimSpace(brief) == "" {
		return "", fmt.Errorf("describe the profession to generate")
	}
	s := Load(root)
	var examples []string
	for _, p := range []string{"developer", "designer", "tester"} {
		body, err := s.PositionText(p)
		if err != nil {
			return "", err
		}
		examples = append(examples, body)
	}
	sectors := map[string]bool{}
	var pools []string
	for _, p := range s.Catalogue() {
		if !sectors[p.Sector] {
			sectors[p.Sector] = true
			pools = append(pools, p.Sector)
		}
	}
	standing, err := s.Text("standing.md", nil)
	if err != nil {
		return "", err
	}
	prompt, err := s.Text("role_generation.md", map[string]string{"NAME": name, "BRIEF": brief, "SECTORS": strings.Join(pools, ", "), "EXAMPLES": strings.Join(examples, "\n\n---\n\n"), "STANDING": standing})
	if err != nil {
		return "", err
	}
	args := cfg.RoleGenerationCommand()
	if len(args) == 0 {
		return "", fmt.Errorf("role_generation_command is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.RoleGenerator.Timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "vcomp-role-generation-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(prompt)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("role generation failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	text := strings.TrimSpace(string(out))
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	if err = s.ValidatePosition(name, text); err != nil {
		return text, fmt.Errorf("generated draft needs editing: %w", err)
	}
	return text + "\n", nil
}
