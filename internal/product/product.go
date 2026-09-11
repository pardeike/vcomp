// Package product provides reusable employee worktrees and publication through Git.
// It does not assign work, review contributions, or restrict ordinary file access.
package product

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
)

type Result struct {
	Status         string   `json:"status"`
	Path           string   `json:"path"`
	Shared         string   `json:"shared"`
	Revision       string   `json:"revision"`
	SharedRevision string   `json:"shared_revision"`
	Unpublished    bool     `json:"unpublished"`
	Conflicts      []string `json:"conflicts,omitempty"`
	Instruction    string   `json:"instruction,omitempty"`
}

type workspace struct{ root, shared, dir, role, branch string }

func open(root, role string) (*workspace, error) {
	if err := bootstrap.RoleName(role); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	employee := filepath.Join(root, "spaces", role)
	if _, err := os.Stat(filepath.Join(employee, "role.md")); err != nil {
		return nil, err
	}
	actual, err := filepath.EvalSymlinks(employee)
	if err != nil {
		return nil, err
	}
	if actual != employee {
		return nil, fmt.Errorf("employee space must be an ordinary directory")
	}
	w := &workspace{root: root, shared: filepath.Join(root, "product"), dir: filepath.Join(employee, "product"), role: role, branch: "vcomp/" + role}
	top, err := git(w.shared, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if top != w.shared {
		return nil, fmt.Errorf("shared product must be its own repository")
	}
	return w, nil
}

// Locks cover only tool operations, never an employee's editing/thinking time.
// A busy caller gets an immediate retryable error instead of an invisible queue.
func lock(root, name string) (func(), error) {
	dir := filepath.Join(config.LocalDir(root), "product-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("product operation busy; retry shortly: %w", err)
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func Work(root, role string) (Result, error) {
	w, err := open(root, role)
	if err != nil {
		return Result{}, err
	}
	unlock, err := lock(w.root, "employee-"+role)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	if err := w.ensure(); err != nil {
		return Result{}, err
	}
	shared, err := git(w.shared, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	dirty, err := git(w.dir, "status", "--porcelain")
	if err != nil {
		return Result{}, err
	}
	// Fast-forward only when there is nothing private to preserve. Never merge
	// into a dirty or divergent copy just because someone asks for its path.
	if dirty == "" && !w.merging() && ancestor(w.dir, "HEAD", shared) {
		if _, err := w.git("merge", "--ff-only", "--no-autostash", shared); err != nil {
			return Result{}, err
		}
	}
	return w.result("ready")
}

func (w *workspace) ensure() error {
	if _, err := os.Lstat(w.dir); os.IsNotExist(err) {
		args := []string{"worktree", "add", "--quiet"}
		if _, err := git(w.shared, "show-ref", "--verify", "refs/heads/"+w.branch); err != nil {
			args = append(args, "-b", w.branch)
			args = append(args, "--", w.dir, "HEAD")
		} else {
			args = append(args, "--", w.dir, w.branch)
		}
		if _, err := git(w.shared, args...); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(w.dir)
	if err != nil {
		return err
	}
	if actual != w.dir {
		return fmt.Errorf("working copy must be an ordinary directory")
	}
	top, err := git(w.dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	common, err := git(w.dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	expected, err := git(w.shared, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	branch, err := git(w.dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	sharedBranch, _ := git(w.shared, "symbolic-ref", "--short", "HEAD")
	if top != w.dir || common != expected || branch == sharedBranch {
		return fmt.Errorf("%s is not this employee's product worktree; existing files were preserved", w.dir)
	}
	return nil
}

func Publish(root, role, summary string) (Result, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" || strings.ContainsAny(summary, "\r\n\x00") {
		return Result{}, fmt.Errorf("provide a nonempty single-line summary")
	}
	w, err := open(root, role)
	if err != nil {
		return Result{}, err
	}
	unlock, err := lock(w.root, "employee-"+role)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	// A mistaken publish should not create a copy or commit somebody else's files.
	if _, err := os.Stat(filepath.Join(w.dir, ".git")); err != nil {
		return Result{}, fmt.Errorf("call product_work first: %w", err)
	}
	if err := w.ensure(); err != nil {
		return Result{}, err
	}
	conflicts, err := w.conflicts()
	if err != nil {
		return Result{}, err
	}
	if len(conflicts) > 0 {
		return w.result("conflict")
	}
	dirty, err := git(w.dir, "status", "--porcelain")
	if err != nil {
		return Result{}, err
	}
	if dirty != "" || w.merging() {
		if _, err := w.git("add", "-A"); err != nil {
			return Result{}, err
		}
		if _, err := w.git("commit", "-m", summary); err != nil {
			return Result{}, err
		}
	}
	// Only publication is serialized across employees. No model or tests run here.
	publishUnlock, err := lock(w.root, "publish")
	if err != nil {
		return Result{}, err
	}
	defer publishUnlock()
	dirty, err = git(w.shared, "status", "--porcelain")
	if err != nil {
		return Result{}, err
	}
	if dirty != "" {
		return Result{}, fmt.Errorf("shared product has unpublished direct edits; preserve or commit those before retrying. Your contribution remains saved in %s", w.dir)
	}
	shared, err := git(w.shared, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	if _, err := w.git("merge", "--no-edit", "--no-autostash", "-m", summary, shared); err != nil {
		conflicts, checkErr := w.conflicts()
		if checkErr == nil && len(conflicts) > 0 {
			return w.result("conflict")
		}
		return Result{}, err
	}
	head, err := git(w.dir, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	if head == shared {
		return w.result("up_to_date")
	}
	if _, err := git(w.shared, "merge", "--ff-only", "--no-autostash", head); err != nil {
		return Result{}, err
	}
	return w.result("published")
}

func (w *workspace) merging() bool {
	_, err := git(w.dir, "rev-parse", "--verify", "MERGE_HEAD")
	return err == nil
}
func (w *workspace) conflicts() ([]string, error) {
	out, err := git(w.dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(out, "\x00"), "\x00"), nil
}
func (w *workspace) result(status string) (Result, error) {
	head, err := git(w.dir, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	shared, err := git(w.shared, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	dirty, err := git(w.dir, "status", "--porcelain")
	if err != nil {
		return Result{}, err
	}
	conflicts, err := w.conflicts()
	if err != nil {
		return Result{}, err
	}
	r := Result{Status: status, Path: w.dir, Shared: w.shared, Revision: head, SharedRevision: shared, Unpublished: dirty != "" || !ancestor(w.dir, head, shared) || w.merging(), Conflicts: conflicts}
	if len(conflicts) > 0 {
		r.Status = "conflict"
		r.Instruction = "Resolve the listed conflicts in your working copy, git add the resolved files, then call product_publish again. The shared product was not changed by this attempt."
	}
	return r, nil
}
func ancestor(dir, a, b string) bool {
	_, err := git(dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}
func (w *workspace) git(args ...string) (string, error) {
	return git(w.dir, append([]string{"-c", "user.name=" + w.role, "-c", "user.email=" + w.role + "@vcomp.local"}, args...)...)
}
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
