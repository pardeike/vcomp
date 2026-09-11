package product

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Snapshot exports one committed revision, even if publication advances HEAD
// during the copy. Private work, build output and direct uncommitted edits stay out.
func Snapshot(root, dst string) (string, error) {
	src := filepath.Join(root, "product")
	version, err := git(src, "log", "-1", "--format=%H %s")
	if err != nil {
		return "", err
	}
	revision := strings.Fields(version)[0]
	archive := exec.Command("git", "-C", src, "archive", "--format=tar", revision)
	var archiveError bytes.Buffer
	archive.Stderr = &archiveError
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := archive.Start(); err != nil {
		return "", err
	}
	extract := exec.Command("tar", "-xf", "-", "-C", dst)
	extract.Stdin = pipe
	output, extractErr := extract.CombinedOutput()
	pipe.Close()
	archiveErr := archive.Wait()
	if archiveErr != nil {
		return "", fmt.Errorf("snapshot archive: %w: %s", archiveErr, archiveError.String())
	}
	if extractErr != nil {
		return "", fmt.Errorf("snapshot extract: %w: %s", extractErr, output)
	}
	return version, nil
}
