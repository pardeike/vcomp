// Package config resolves every knob the simulation has: timings, harness
// command lines, models, efforts, the starting roster, the prompts typed at the
// agents, and per-role overrides.
//
// Settings come from three layers, each overriding the one before it:
//
//	built-in defaults    compiled into the binary
//	~/.vcomp/vcomp.conf  your defaults for every company (see "vcomp install")
//	<root>/.vcomp/…      this company's overrides, usually a handful of lines
//
// So an empty directory is already a valid company root.
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"vcomp/internal/space"
)

//go:embed default.conf
var defaultConf string

const (
	// FileName is the config file, in ~/.vcomp/ or in a company's .vcomp/.
	FileName = "vcomp.conf"
	// DirName is the per-company settings and state directory.
	DirName = ".vcomp"
	// TemplatesDir holds role and document templates within a settings dir.
	TemplatesDir = "templates"
	// HomeEnv overrides where the global defaults live, mostly for tests.
	HomeEnv = "VCOMP_HOME"
)

// Home is the global settings directory: $VCOMP_HOME, or ~/.vcomp.
func Home() string {
	if d := os.Getenv(HomeEnv); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return DirName
	}
	return filepath.Join(home, DirName)
}

// LocalDir is a company's own settings and state directory.
func LocalDir(root string) string { return filepath.Join(root, DirName) }

// Harness is how to start a CLI agent, and how to start it again so that it
// keeps the memory of its previous session.
type Harness struct {
	Start  []string
	Resume []string
	Model  string
	Effort string
	// Models and Efforts are the values the interface offers in its choosers;
	// they are suggestions only, and any other value can still be typed.
	Models  []string
	Efforts []string
	// Handshake is tmux key names sent once after the session comes up, before
	// the first prompt - for whatever a harness asks before it will talk.
	Handshake []string
	// TurnHistory is an OMP terminal-sessions directory, empty when unsupported.
	TurnHistory string
}

// Role holds per-person overrides. Zero fields fall back to the defaults.
// The two idle counts are the throttle: raising them for one role slows how
// often that person is prodded back into action.
type Role struct {
	Harness        string
	Model          string
	Effort         string
	IdleTicks      int
	IdleTicksEmpty int
	Prompts        map[string]string
}

type RoleGenerator struct {
	Command       []string
	Model, Effort string
	Timeout       time.Duration
}

type Config struct {
	TerminalView     string
	UISort           map[string]string
	RoleGenerator    RoleGenerator
	Tick             time.Duration
	TUIRefresh       time.Duration
	IdleTicks        int
	IdleTicksEmpty   int
	UserTimeout      time.Duration
	UserMaxAttempts  int
	MaxRestarts      int
	LaunchRetryDelay time.Duration

	SessionPrefix       string
	CEOInstructionsFile string
	GoalFile            string
	StopTimeout         time.Duration
	StopPoll            time.Duration
	Goal                string
	ResultFile          string
	StateFile           string
	AuditFile           string
	Harness             string
	Harnesses           map[string]Harness
	Prompts             map[string]string
	Roster              []string
	Roles               map[string]Role
	User                Role // Public tester overrides; only harness, model and effort apply.
}

// Prompt kinds.
const (
	PromptFresh     = "fresh"
	PromptBack      = "back"
	PromptNudge     = "nudge"
	PromptUser      = "user"
	PromptUserNudge = "user_nudge"
)

var promptKinds = map[string]bool{
	PromptFresh: true, PromptBack: true, PromptNudge: true,
	PromptUser: true, PromptUserNudge: true,
}

// Default is the built-in configuration, which is exactly what DefaultText
// writes, so the shipped file and the defaults can never drift apart.
func Default() Config {
	c := empty()
	if err := c.Merge(defaultConf); err != nil {
		panic("built-in default.conf is broken: " + err.Error()) // build-time bug
	}
	return c
}

// DefaultText is the commented config file installed as the global defaults.
func DefaultText() string { return defaultConf }

// Files lists the config files that apply to a company root, in the order they
// are applied. Missing ones are skipped.
func Files(root string) []string {
	return []string{
		filepath.Join(Home(), FileName),
		filepath.Join(LocalDir(root), FileName),
	}
}

// Load resolves the built-in defaults, then the global ones, then this
// company's. A root with no settings at all is valid and yields the defaults.
func Load(root string) (Config, error) {
	c := Default()
	for _, p := range Files(root) {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return c, err
		}
		if err := c.Merge(string(b)); err != nil {
			return c, fmt.Errorf("%s: %w", p, err)
		}
	}
	if c.GoalFile != "" {
		b, err := os.ReadFile(filepath.Join(root, c.GoalFile))
		if err != nil {
			return c, fmt.Errorf("goal_file: %w", err)
		}
		c.Goal = string(b)
	}
	return c, nil
}

// Parse reads a configuration from text alone, with no defaults underneath it.
func Parse(text string) (Config, error) {
	c := empty()
	return c, c.Merge(text)
}

func empty() Config {
	return Config{
		Harnesses: map[string]Harness{},
		Prompts:   map[string]string{},
		Roles:     map[string]Role{},
	}
}

// Merge applies a config file's settings on top of what is already there. The
// format is "key = value" lines grouped by optional "[kind name]" headers, with
// whole-line "#" comments only, so a value may contain "#" and "=".
func (c *Config) Merge(text string) error {
	kind, name := "", ""
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		at := func(format string, a ...any) error {
			return fmt.Errorf("line %d: %s", n+1, fmt.Sprintf(format, a...))
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			head := strings.Fields(strings.Trim(line, "[]"))
			if len(head) == 0 {
				return at("empty section header")
			}
			kind, name = head[0], strings.Join(head[1:], " ")
			switch kind {
			case "prompts":
			case "user":
				if name != "" {
					return at("[user] does not take a name")
				}
			case "harness", "role":
				if name == "" {
					return at("[%s] needs a name", kind)
				}
			default:
				return at("unknown section %q", kind)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return at("expected 'key = value'")
		}
		if err := c.set(kind, name, strings.TrimSpace(key), strings.TrimSpace(value)); err != nil {
			return at("%v", err)
		}
	}
	return nil
}

func (c *Config) set(kind, name, key, value string) error {
	switch kind {
	case "":
		return c.setTop(key, value)
	case "prompts":
		if !promptKinds[key] {
			return fmt.Errorf("unknown prompt %q", key)
		}
		c.Prompts[key] = value
	case "user":
		switch key {
		case "harness":
			c.User.Harness = value
		case "model":
			c.User.Model = value
		case "effort":
			c.User.Effort = value
		default:
			return fmt.Errorf("unknown user key %q", key)
		}
	case "harness":
		h := c.Harnesses[name]
		switch key {
		case "start":
			h.Start = strings.Fields(value)
		case "resume":
			h.Resume = strings.Fields(value)
		case "model":
			h.Model = value
		case "effort":
			h.Effort = value
		case "models":
			h.Models = list(value)
		case "efforts":
			h.Efforts = list(value)
		case "turn_history":
			h.TurnHistory = value
		case "handshake":
			h.Handshake = strings.Fields(value)
		default:
			return fmt.Errorf("unknown harness key %q", key)
		}
		c.Harnesses[name] = h
	case "role":
		r, ok := c.Roles[name]
		if !ok {
			r = Role{Prompts: map[string]string{}}
		}
		switch {
		case key == "harness":
			r.Harness = value
		case key == "model":
			r.Model = value
		case key == "effort":
			r.Effort = value
		case key == "idle_ticks", key == "idle_ticks_empty":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: %v", key, err)
			}
			if key == "idle_ticks" {
				r.IdleTicks = n
			} else {
				r.IdleTicksEmpty = n
			}
		case promptKinds[key]:
			r.Prompts[key] = value
		default:
			return fmt.Errorf("unknown role key %q", key)
		}
		c.Roles[name] = r
	}
	return nil
}

func (c *Config) setTop(key, value string) error {
	dur := func(d *time.Duration) error {
		v, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
		if v <= 0 {
			return fmt.Errorf("%s must be positive", key)
		}
		*d = v
		return nil
	}
	num := func(n *int) error {
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
		*n = v
		return nil
	}
	switch key {
	case "dashboard_sort", "catalogue_sort", "public_tests_sort":
		field := strings.TrimPrefix(value, "-")
		valid := false
		for _, option := range SortFields(key) {
			if field == option {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("invalid %s value %q", key, value)
		}
		if c.UISort == nil {
			c.UISort = map[string]string{}
		}
		c.UISort[key] = value
	case "role_generation_command":
		c.RoleGenerator.Command = strings.Fields(value)
	case "role_generation_model":
		c.RoleGenerator.Model = value
	case "role_generation_effort":
		c.RoleGenerator.Effort = value
	case "role_generation_timeout":
		return dur(&c.RoleGenerator.Timeout)
	case "terminal_view":
		if value != "brief" && value != "detailed" && value != "raw" {
			return fmt.Errorf("terminal_view must be brief, detailed or raw")
		}
		c.TerminalView = value
	case "tui_refresh":
		return dur(&c.TUIRefresh)
	case "tick":
		return dur(&c.Tick)
	case "stop_timeout":
		return dur(&c.StopTimeout)
	case "stop_poll":
		return dur(&c.StopPoll)
	case "user_timeout":
		return dur(&c.UserTimeout)
	case "idle_ticks":
		return num(&c.IdleTicks)
	case "idle_ticks_empty":
		return num(&c.IdleTicksEmpty)
	case "user_max_attempts":
		return num(&c.UserMaxAttempts)
	case "launch_retry_delay":
		return dur(&c.LaunchRetryDelay)
	case "max_restarts":
		return num(&c.MaxRestarts)
	case "session_prefix":
		if value == "" || strings.ContainsAny(value, ".:/\\ \t\n") {
			return fmt.Errorf("session_prefix needs a nonempty tmux-compatible name")
		}
		c.SessionPrefix = value
	case "goal":
		c.Goal, c.GoalFile = value, ""
	case "ceo_instructions_file":
		c.CEOInstructionsFile = value
	case "goal_file":
		c.GoalFile, c.Goal = value, ""
	case "result_file":
		c.ResultFile = value
	case "state_file":
		c.StateFile = value
	case "audit_file":
		c.AuditFile = value
	case "harness":
		c.Harness = value
	case "roster":
		c.Roster = list(value)
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// Valid reports whether a proposed setting would parse, without applying it.
func Valid(section, name, key, value string) error {
	c := empty()
	return c.set(section, name, key, value)
}

// HarnessFor names the harness a role runs under; an empty role is a public tester.
// A conversation started in one harness cannot be resumed in another.
func (c Config) HarnessFor(role string) string {
	return firstNonEmpty(c.agentOverrides(role).Harness, c.Harness)
}

func (c Config) agentOverrides(role string) Role {
	if role == "" {
		return c.User
	}
	return c.Roles[role]
}

// Handshake is the key sequence a role's harness needs before it will accept a
// prompt, such as answering a first-run trust question.
func (c Config) Handshake(role string) []string {
	return c.Harnesses[c.HarnessFor(role)].Handshake
}

// CommandFor builds the argv that starts a role's agent, applying the role's
// harness, model and effort overrides. An empty role selects the public tester.
func (c Config) CommandFor(role string, resume bool) ([]string, error) {
	r := c.agentOverrides(role)
	hname := firstNonEmpty(r.Harness, c.Harness)
	h, ok := c.Harnesses[hname]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q", hname)
	}
	tmpl := h.Start
	if resume && len(h.Resume) > 0 {
		tmpl = h.Resume
	}
	if len(tmpl) == 0 {
		return nil, fmt.Errorf("harness %q has no command", hname)
	}
	return expand(tmpl, map[string]string{
		"model":  firstNonEmpty(r.Model, h.Model),
		"effort": firstNonEmpty(r.Effort, h.Effort),
	}), nil
}

// Prompt returns the text to type at a role, honouring per-role overrides.
// role may be empty for prompts that are not about a specific person.
func (c Config) Prompt(role, kind string) string {
	if r, ok := c.Roles[role]; ok {
		if v := r.Prompts[kind]; v != "" {
			return v
		}
	}
	return c.Prompts[kind]
}

// IdleThreshold is how many motionless ticks mean a role needs prodding.
// Someone with an empty inbox is left alone for longer: they have nothing
// waiting for them, and the point is to keep the company busy, not to keep
// everyone manufacturing work for themselves.
func (c Config) IdleThreshold(role string, inboxEmpty bool) int {
	r := c.Roles[role]
	if inboxEmpty {
		return firstPositive(r.IdleTicksEmpty, c.IdleTicksEmpty, r.IdleTicks, c.IdleTicks, 1)
	}
	return firstPositive(r.IdleTicks, c.IdleTicks, 1)
}

func firstPositive(ns ...int) int {
	for _, n := range ns {
		if n > 0 {
			return n
		}
	}
	return 1
}

// Override is one setting to write into a company's own config file. Section is
// "" for a top-level key, or "role NAME" / "harness NAME" / "user" / "prompts".
type Override struct {
	Section string
	Key     string
	Value   string
}

// RenderOverrides writes the minimal config file expressing these settings.
// Anything not listed stays inherited, which is the point: a company's own
// config should be short enough to read at a glance.
func RenderOverrides(overrides []Override) string {
	var b strings.Builder
	b.WriteString("# This company's settings. Everything not named here is inherited from\n")
	b.WriteString("# " + filepath.Join(Home(), FileName) + ", and then from vcomp's built-in defaults.\n")

	b.WriteString(renderSettings(overrides))
	return b.String()
}

func renderSettings(overrides []Override) string {
	var b strings.Builder
	bySection := map[string][]Override{}
	var order []string
	for _, o := range overrides {
		if _, seen := bySection[o.Section]; !seen {
			order = append(order, o.Section)
		}
		bySection[o.Section] = append(bySection[o.Section], o)
	}
	// Top-level keys have to come before any section header.
	sort.SliceStable(order, func(i, j int) bool { return order[i] == "" && order[j] != "" })

	for _, section := range order {
		if section == "" {
			b.WriteString("\n")
		} else {
			b.WriteString("\n[" + section + "]\n")
		}
		for _, o := range bySection[section] {
			b.WriteString(o.Key + " = " + o.Value + "\n")
		}
	}
	return b.String()
}

// barePlaceholder matches a token that is nothing but a placeholder, which
// means it is the value belonging to the flag in front of it.
var barePlaceholder = regexp.MustCompile(`^\{\{\w+\}\}$`)

// expand substitutes {{model}} and {{effort}} and removes whatever an empty
// value leaves behind: the token itself, plus the flag it belonged to when the
// token was a bare placeholder. So "--model {{model}}" and "--model={{model}}"
// both vanish entirely when no model is set, rather than passing an empty
// argument or an orphaned flag.
func expand(tokens []string, vals map[string]string) []string {
	var out, from []string
	for _, t := range tokens {
		v, drop := substitute(t, vals)
		if drop {
			if barePlaceholder.MatchString(t) && len(out) > 0 {
				if prev := from[len(from)-1]; strings.HasPrefix(prev, "-") && !strings.Contains(prev, "{{") {
					out, from = out[:len(out)-1], from[:len(from)-1]
				}
			}
			continue
		}
		if v == "" {
			continue
		}
		out, from = append(out, v), append(from, t)
	}
	return out
}

// substitute fills in a token's placeholders, reporting whether one of them was
// empty and the token therefore has to go.
func substitute(t string, vals map[string]string) (string, bool) {
	for k, v := range vals {
		ph := "{{" + k + "}}"
		if !strings.Contains(t, ph) {
			continue
		}
		if v == "" {
			return "", true
		}
		t = strings.ReplaceAll(t, ph, v)
	}
	return t, false
}

// list splits a comma-separated value, dropping blanks.
func list(value string) []string {
	var out []string
	for _, s := range strings.Split(value, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// UpdateLocal preserves existing settings, including those setup does not ask
// about. Repeated sections are accepted by the same parser as normal config.
func UpdateLocal(root string, updates []Override) error {
	return updateFile(filepath.Join(LocalDir(root), FileName), updates)
}
func UpdateGlobal(updates []Override) error {
	return updateFile(filepath.Join(Home(), FileName), updates)
}
func updateFile(p string, updates []Override) error {
	b, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := Parse(string(b)); err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	section := ""
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Join(strings.Fields(strings.Trim(line, "[]")), " ")
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		for _, u := range updates {
			if section == u.Section && (key == u.Key || section == "" &&
				(key == "goal" && u.Key == "goal_file" || key == "goal_file" && u.Key == "goal")) {
				lines[i] = "" // the new value is written once below
			}
		}
	}
	var top, rest []Override
	for _, u := range updates {
		if u.Section == "" {
			top = append(top, u)
		} else {
			rest = append(rest, u)
		}
	}
	text := renderSettings(top) + "\n" + strings.Join(lines, "\n") + "\n" + renderSettings(rest)
	if _, err := Parse(text); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	return space.WriteFile(p, []byte(text), 0644)
}

func (c Config) RoleGenerationCommand() []string {
	return expand(c.RoleGenerator.Command, map[string]string{"model": c.RoleGenerator.Model, "effort": c.RoleGenerator.Effort})
}

// SortFields are display choices only; the engine never orders work with them.
func SortFields(key string) []string {
	switch key {
	case "dashboard_sort":
		return []string{"name", "state", "inbox", "turns", "started", "average", "harness"}
	case "catalogue_sort":
		return []string{"name", "title", "sector", "state"}
	case "public_tests_sort":
		return []string{"name", "created", "state", "attempts"}
	}
	return nil
}
