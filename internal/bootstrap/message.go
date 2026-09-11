package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"vcomp/internal/space"
)

// Message publishes a complete ordinary inbox request, without touching sessions.
func Message(root, name, subject, body string) (string, error) {
	if err := RoleName(name); err != nil {
		return "", err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || strings.ContainsAny(subject, "\r\n") || strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("provide a single-line subject and a nonempty message")
	}
	roles, err := space.Roles(root)
	if err != nil {
		return "", err
	}
	dir := ""
	for _, r := range roles {
		if r.Name == name {
			dir = r.Dir
			break
		}
	}
	if dir == "" {
		return "", fmt.Errorf("employee %q does not exist", name)
	}
	text, err := Load(root).Text("user_message.md", map[string]string{"SUBJECT": subject, "BODY": body})
	if err != nil {
		return "", err
	}
	var safe strings.Builder
	for _, r := range subject {
		if safe.Len() >= 80 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ' ' {
			safe.WriteRune(r)
		} else {
			safe.WriteByte('-')
		}
	}
	inbox := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inbox, 0755); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(dir, ".user-message-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := os.WriteFile(filepath.Join(staging, "message.md"), []byte(text), 0644); err != nil {
		return "", err
	}
	suffix := strings.TrimPrefix(filepath.Base(staging), ".user-message-")
	target := filepath.Join(inbox, "URGENT - FROM USER - "+strings.TrimSpace(safe.String())+" - "+suffix)
	if err := os.Rename(staging, target); err != nil {
		return "", err
	}
	return target, nil
}
