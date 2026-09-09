// Package space maps the company folder layout onto Go values. The filesystem
// is the source of truth: a space with a role.md is an employee, a public run
// folder without impressions.md is a pending user test.
package space

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	SpacesDir  = "spaces"
	PublicDir  = "public"
	ProductDir = "product"
	RoleFile   = "role.md"
)

// Role is one employee: a folder under spaces/ containing a role.md.
type Role struct {
	Name string
	Dir  string
	Hash string // of role.md; a change means the CEO replaced this person
}

// Session is the tmux session this role runs in.
func (r Role) Session(prefix string) string { return prefix + "-" + r.Name }
func (r Role) Inbox() string                { return filepath.Join(r.Dir, "inbox") }

// Roles lists every employee, sorted by name.
func Roles(root string) ([]Role, error) {
	base := filepath.Join(root, SpacesDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var roles []Role
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(base, e.Name())
		b, err := os.ReadFile(filepath.Join(dir, RoleFile))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			if _, err := os.Stat(filepath.Join(dir, "role.json")); err != nil {
				continue
			}
		}
		roles = append(roles, Role{Name: e.Name(), Dir: dir, Hash: Hash(b)})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles, nil
}

// Run is one public user test.
type Run struct {
	Name string
	Dir  string
}

func (r Run) Session(prefix string) string { return prefix + "-user-" + r.Name }
func (r Run) Impressions() string          { return filepath.Join(r.Dir, "impressions.md") }
func (r Run) Abandoned() string            { return filepath.Join(r.Dir, "abandoned.txt") }

func (r Run) Done() bool    { return exists(r.Impressions()) }
func (r Run) GivenUp() bool { return exists(r.Abandoned()) }

// Runs lists every public run folder, sorted by name (which is chronological).
func Runs(root string) []Run {
	entries, err := os.ReadDir(filepath.Join(root, PublicDir))
	if err != nil {
		return nil
	}
	var runs []Run
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "run-") {
			continue
		}
		runs = append(runs, Run{Name: e.Name(), Dir: filepath.Join(root, PublicDir, e.Name())})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Name < runs[j].Name })
	return runs
}

// NextRunName returns the name for a new public run folder.
func NextRunName(root string) string {
	n := 0
	for _, r := range Runs(root) {
		var i int
		if _, err := fmt.Sscanf(r.Name, "run-%d", &i); err == nil && i > n {
			n = i
		}
	}
	return fmt.Sprintf("run-%04d", n+1)
}

// Hash fingerprints file content, to notice that it changed.
func Hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// WriteFile publishes a complete file in one rename. Readers see either the old
// contents or the new contents, never a partly written state or role document.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".vcomp-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
