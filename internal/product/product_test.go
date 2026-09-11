package product

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	for _, role := range []string{"alice", "bob"} {
		p := filepath.Join(root, "spaces", role)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(p, "role.md"), role)
	}
	shared := filepath.Join(root, "product")
	os.MkdirAll(shared, 0755)
	mustGit(t, shared, "init", "-q", "-b", "main")
	write(t, filepath.Join(shared, "shared.txt"), "original\n")
	write(t, filepath.Join(shared, ".gitignore"), ".build/\n")
	mustGit(t, shared, "add", ".")
	commit(t, shared, "Initial")
	return root
}
func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func commit(t *testing.T, dir, summary string) {
	t.Helper()
	mustGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@localhost", "commit", "-m", summary)
}
func work(t *testing.T, root, role string) Result {
	t.Helper()
	r, err := Work(root, role)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func publish(t *testing.T, root, role, summary string) Result {
	t.Helper()
	r, err := Publish(root, role, summary)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestIndependentContributionsAndRefresh(t *testing.T) {
	root := fixture(t)
	a := work(t, root, "alice")
	b := work(t, root, "bob")
	write(t, filepath.Join(a.Path, "alice.txt"), "alice's contribution\n")
	write(t, filepath.Join(b.Path, "bob.txt"), "bob's contribution\n")
	if _, err := os.Stat(filepath.Join(a.Shared, "alice.txt")); !os.IsNotExist(err) {
		t.Fatal("private edit reached shared product")
	}
	publish(t, root, "alice", "Alice contribution")
	if refreshed := work(t, root, "bob"); !refreshed.Unpublished || read(t, filepath.Join(b.Path, "bob.txt")) != "bob's contribution\n" {
		t.Fatal("dirty work was lost")
	}
	r := publish(t, root, "bob", "Bob contribution")
	if r.Status != "published" || r.Unpublished {
		t.Fatal(r)
	}
	for _, file := range []string{"alice.txt", "bob.txt"} {
		if _, err := os.Stat(filepath.Join(a.Shared, file)); err != nil {
			t.Fatal(err)
		}
	}
	if work(t, root, "alice").Revision != r.Revision {
		t.Fatal("published copy did not refresh")
	}
	if got := mustGit(t, a.Shared, "log", "--format=%an"); !strings.Contains(got, "alice") || !strings.Contains(got, "bob") {
		t.Fatal(got)
	}
}

func TestConflictPreservesBothHistoriesAndCanBeResolved(t *testing.T) {
	root := fixture(t)
	a := work(t, root, "alice")
	b := work(t, root, "bob")
	write(t, filepath.Join(a.Path, "shared.txt"), "alice\n")
	write(t, filepath.Join(b.Path, "shared.txt"), "bob\n")
	first := publish(t, root, "alice", "Alice changes shared")
	conflicted := publish(t, root, "bob", "Bob changes shared")
	if conflicted.Status != "conflict" || len(conflicted.Conflicts) != 1 || conflicted.Conflicts[0] != "shared.txt" {
		t.Fatal(conflicted)
	}
	if got := read(t, filepath.Join(a.Shared, "shared.txt")); got != "alice\n" {
		t.Fatal("shared file damaged", got)
	}
	if mustGit(t, a.Shared, "rev-parse", "HEAD") != first.Revision {
		t.Fatal("conflict moved shared HEAD")
	}
	if got := read(t, filepath.Join(b.Path, "shared.txt")); !strings.Contains(got, "alice") || !strings.Contains(got, "bob") {
		t.Fatal("lost one contribution", got)
	}
	if again := publish(t, root, "bob", "Still unresolved"); again.Status != "conflict" {
		t.Fatal("unresolved conflict was published")
	}
	if again := work(t, root, "bob"); again.Status != "conflict" {
		t.Fatal("work erased merge state")
	}
	write(t, filepath.Join(b.Path, "shared.txt"), "alice and bob\n")
	mustGit(t, b.Path, "add", "shared.txt")
	resolved := publish(t, root, "bob", "Keep both changes")
	if resolved.Status != "published" || read(t, filepath.Join(a.Shared, "shared.txt")) != "alice and bob\n" {
		t.Fatal(resolved)
	}
	if !ancestor(a.Shared, first.Revision, resolved.Revision) || !ancestor(a.Shared, conflicted.Revision, resolved.Revision) {
		t.Fatal("lost history")
	}
}

func TestPrivateCommitsDirtySharedAndFailedPublicationStayRecoverable(t *testing.T) {
	root := fixture(t)
	a := work(t, root, "alice")
	write(t, filepath.Join(a.Path, "private.txt"), "private\n")
	mustGit(t, a.Path, "add", ".")
	commit(t, a.Path, "Private commit")
	private := mustGit(t, a.Path, "rev-parse", "HEAD")
	if refreshed := work(t, root, "alice"); refreshed.Revision != private || !refreshed.Unpublished {
		t.Fatal("private commit reset", refreshed)
	}
	write(t, filepath.Join(a.Shared, "direct.txt"), "direct edit\n")
	if _, err := Publish(root, "alice", "Publish"); err == nil {
		t.Fatal("dirty shared checkout accepted")
	}
	if read(t, filepath.Join(a.Shared, "direct.txt")) != "direct edit\n" || mustGit(t, a.Path, "rev-parse", "HEAD") != private {
		t.Fatal("failed publication destroyed work")
	}
	mustGit(t, a.Shared, "add", "direct.txt")
	commit(t, a.Shared, "Preserve direct edit")
	if publish(t, root, "alice", "Publish private commit").Status != "published" {
		t.Fatal("cannot retry")
	}
}

func TestConcurrentPublishPreservesBothContributions(t *testing.T) {
	root := fixture(t)
	a := work(t, root, "alice")
	b := work(t, root, "bob")
	write(t, filepath.Join(a.Path, "a"), "A")
	write(t, filepath.Join(b.Path, "b"), "B")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, role := range []string{"alice", "bob"} {
		wg.Add(1)
		go func(i int, role string) { defer wg.Done(); _, errs[i] = Publish(root, role, role) }(i, role)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			if !strings.Contains(err.Error(), "busy") {
				t.Fatal(err)
			}
			publish(t, root, []string{"alice", "bob"}[i], "Retry")
		}
	}
	if read(t, filepath.Join(a.Shared, "a")) != "A" || read(t, filepath.Join(a.Shared, "b")) != "B" {
		t.Fatal("contribution lost")
	}
}

func TestCompanyScopeAndExistingFolder(t *testing.T) {
	one, two := fixture(t), fixture(t)
	a := work(t, one, "alice")
	b := work(t, two, "alice")
	write(t, filepath.Join(a.Path, "one"), "first company")
	publish(t, one, "alice", "First only")
	if _, err := os.Stat(filepath.Join(b.Shared, "one")); !os.IsNotExist(err) {
		t.Fatal("crossed companies")
	}
	dir := filepath.Join(one, "spaces", "bob", "product")
	os.MkdirAll(dir, 0755)
	write(t, filepath.Join(dir, "keep"), "existing")
	if _, err := Work(one, "bob"); err == nil {
		t.Fatal("accepted existing unrelated folder")
	}
	if read(t, filepath.Join(dir, "keep")) != "existing" {
		t.Fatal("lost preexisting file")
	}
	if _, err := Work(one, "../alice"); err == nil {
		t.Fatal("accepted invalid identity")
	}
}

func TestSnapshotUsesPublishedCommit(t *testing.T) {
	root := fixture(t)
	a := work(t, root, "alice")
	write(t, filepath.Join(a.Path, "private"), "not published")
	write(t, filepath.Join(a.Shared, "shared.txt"), "dirty direct edit")
	write(t, filepath.Join(a.Shared, "untracked"), "untracked")
	dst := t.TempDir()
	version, err := Snapshot(root, dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, mustGit(t, a.Shared, "rev-parse", "HEAD")) {
		t.Fatal(version)
	}
	if read(t, filepath.Join(dst, "shared.txt")) != "original\n" {
		t.Fatal("snapshot included dirty content")
	}
	for _, file := range []string{"private", "untracked", ".git"} {
		if _, err := os.Stat(filepath.Join(dst, file)); !os.IsNotExist(err) {
			t.Fatal("unexpected snapshot file", file)
		}
	}
}

func TestEmployeeCanUseTheirOwnBranchName(t *testing.T) {
	root := fixture(t)
	a := work(t, root, "alice")
	mustGit(t, a.Path, "switch", "-c", "fish-art")
	write(t, filepath.Join(a.Path, "art"), "fish")
	if r := publish(t, root, "alice", "Fish art"); r.Status != "published" {
		t.Fatal(r)
	}
}
