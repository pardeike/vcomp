// Package tui is the interactive operator interface. It never decides company
// work; actions use the same operations as the plain command-line interface.
package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

func Interactive() bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		c := exec.Command("stty", "-g")
		c.Stdin = f
		if _, err := c.Output(); err != nil {
			return false
		}
	}
	return true
}

type key struct{ Name, Text string }
type terminal struct {
	tty   *os.File
	saved string
	keys  chan key
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

func openTerminal() (*terminal, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	t := &terminal{tty: f, keys: make(chan key, 128), stop: make(chan struct{}), done: make(chan struct{})}
	c := exec.Command("stty", "-g")
	c.Stdin = f
	b, err := c.Output()
	if err != nil {
		f.Close()
		return nil, err
	}
	t.saved = strings.TrimSpace(string(b))
	c = exec.Command("stty", "raw", "-echo", "min", "0", "time", "1")
	c.Stdin = f
	if err = c.Run(); err != nil {
		f.Close()
		return nil, err
	}
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l\x1b[?7l\x1b[?2004h")
	go t.readKeys()
	return t, nil
}
func (t *terminal) Close() {
	t.once.Do(func() {
		close(t.stop)
		<-t.done
		c := exec.Command("stty", t.saved)
		c.Stdin = t.tty
		_ = c.Run()
		fmt.Fprint(os.Stdout, "\x1b[0m\x1b[?2004l\x1b[?7h\x1b[?25h\x1b[?1049l")
		t.tty.Close()
	})
}
func (t *terminal) Size() (int, int) {
	c := exec.Command("stty", "size")
	c.Stdin = t.tty
	b, err := c.Output()
	w, h := 80, 24
	if err == nil {
		var rows, cols int
		if _, err = fmt.Sscanf(string(b), "%d %d", &rows, &cols); err == nil && rows > 0 && cols > 0 {
			w, h = cols, rows
		}
	}
	return w, h
}
func (t *terminal) readKeys() {
	defer close(t.done)
	pending := []byte{}
	buf := make([]byte, 4096)
	for {
		select {
		case <-t.stop:
			return
		default:
		}
		n, err := t.tty.Read(buf)
		if err != nil && err != io.EOF {
			select {
			case t.keys <- key{Name: "eof"}:
			case <-t.stop:
			}
			return
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			k, used := decodeKey(pending, n == 0)
			if used == 0 {
				break
			}
			pending = pending[used:]
			if k.Name != "" || k.Text != "" {
				select {
				case t.keys <- k:
				case <-t.stop:
					return
				}
			}
		}
		if n == 0 && err != nil {
			time.Sleep(time.Millisecond)
		}
	}
}
func decodeKey(b []byte, flush bool) (key, int) {
	if bytes.HasPrefix(b, []byte("\x1b[200~")) {
		end := bytes.Index(b[6:], []byte("\x1b[201~"))
		if end < 0 {
			return key{}, 0
		}
		return key{Name: "paste", Text: clean(string(b[6 : 6+end]))}, end + 12
	}
	if b[0] == 27 {
		sequences := map[string]string{"\x1b[A": "up", "\x1b[B": "down", "\x1b[C": "right", "\x1b[D": "left", "\x1b[Z": "backtab", "\x1b[5~": "pgup", "\x1b[6~": "pgdown", "\x1b[H": "home", "\x1b[F": "end", "\x1bOH": "home", "\x1bOF": "end", "\x1b[3~": "delete", "\x1b[1~": "home", "\x1b[4~": "end", "\x1bOA": "up", "\x1bOB": "down", "\x1bOC": "right", "\x1bOD": "left"}
		for seq, name := range sequences {
			if bytes.HasPrefix(b, []byte(seq)) {
				return key{Name: name}, len(seq)
			}
		}
		if len(b) > 1 && (b[1] == '[' || b[1] == 'O') {
			for i := 2; i < len(b); i++ {
				if b[i] >= 0x40 && b[i] <= 0x7e {
					return key{}, i + 1
				}
			}
			if flush {
				return key{}, len(b)
			}
			return key{}, 0
		}
		if len(b) == 1 && !flush {
			return key{}, 0
		}
		return key{Name: "esc"}, 1
	}
	controls := map[byte]string{3: "ctrl-c", 4: "ctrl-d", 9: "tab", 10: "enter", 13: "enter", 19: "save", 21: "clear", 26: "ctrl-z", 127: "backspace", 8: "backspace"}
	if name, ok := controls[b[0]]; ok {
		return key{Name: name}, 1
	}
	if b[0] < 32 {
		return key{}, 1
	}
	if !utf8.FullRune(b) {
		if flush {
			return key{}, len(b)
		}
		return key{}, 0
	}
	r, n := utf8.DecodeRune(b)
	return key{Text: string(r)}, n
}

// Drop terminal control sequences from untrusted terminal/file content. Newlines
// survive for document layout; escape sequences never reach the user's terminal.
func clean(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 27 {
			i++
			if i >= len(s) {
				break
			}
			if s[i] == '[' {
				i++
				for i < len(s) {
					c := s[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
				continue
			}
			if s[i] == ']' || s[i] == 'P' || s[i] == '_' {
				i++
				for i < len(s) {
					if s[i] == 7 {
						i++
						break
					}
					if s[i] == 27 && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		if r == '\n' {
			b.WriteRune(r)
		} else if r == '\t' {
			b.WriteString("    ")
		} else if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func cells(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200d || r == 0xfe0f {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe10 && r <= 0xfe6f) || (r >= 0xff01 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}
func width(s string) int {
	n := 0
	for _, r := range s {
		n += cells(r)
	}
	return n
}
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = strings.ReplaceAll(clean(s), "\n", " ")
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := cells(r)
		if used+w > n {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}
func pad(s string, n int) string { s = clip(s, n); return s + strings.Repeat(" ", max(0, n-width(s))) }
func wrap(s string, n int) []string {
	if n < 1 {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(clean(s), "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		var b strings.Builder
		used := 0
		for _, r := range line {
			w := cells(r)
			if used+w > n {
				lines = append(lines, b.String())
				b.Reset()
				used = 0
			}
			b.WriteRune(r)
			used += w
		}
		lines = append(lines, b.String())
	}
	return lines
}
