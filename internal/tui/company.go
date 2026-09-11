package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/engine"
	"vcomp/internal/space"
)

func readDocument(path string, tail bool) string {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "Not written yet."
	}
	if err != nil {
		return err.Error()
	}
	defer f.Close()
	const limit = 256 * 1024
	info, err := f.Stat()
	if err != nil {
		return err.Error()
	}
	truncated := info.Size() > limit
	if truncated && tail {
		_, _ = f.Seek(-limit, io.SeekEnd)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return err.Error()
	}
	s := string(b)
	if truncated {
		if tail {
			s = "[Showing the latest 256 KiB]\n" + s
		} else {
			s += "\n[Showing the first 256 KiB]"
		}
	}
	return s
}
func gitText(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", append([]string{"-C", filepath.Join(root, space.ProductDir)}, args...)...)
	b, err := c.CombinedOutput()
	if err != nil && len(b) == 0 {
		return err.Error()
	}
	return strings.TrimSpace(string(b))
}
func loadData(root string) data {
	d := data{Inboxes: map[string][]inboxRequest{}, View: engine.Observe(root), AgentDocs: map[string][]string{}, RunDocs: map[string]string{}, Positions: bootstrap.Load(root).Positions()}
	d.Goal = readDocument(filepath.Join(root, space.SpacesDir, "ceo", "goal.md"), false)
	if d.View.Config.ResultFile != "" {
		d.Result = readDocument(filepath.Join(root, d.View.Config.ResultFile), false)
	} else {
		d.Result = "No result file configured."
	}
	d.Log = readDocument(filepath.Join(config.LocalDir(root), "engine.log"), true)
	d.Settings = "COMPANY OVERRIDES\n" + filepath.Join(config.LocalDir(root), config.FileName) + "\n\n" + readDocument(filepath.Join(config.LocalDir(root), config.FileName), false) + "\n\nEnter / c: change common settings\ne: edit the full file\n\nResolution: built-in defaults, then ~/.vcomp, then this company.\nChanges are loaded at engine ticks. Model/harness changes apply on restart."
	if d.View.Exists {
		status := gitText(root, "status", "--short", "--branch")
		commits := gitText(root, "log", "-12", "--format=%h %s")
		if strings.HasPrefix(commits, "fatal:") {
			commits = "No commits yet."
		}
		d.RecentCommits = strings.Split(commits, "\n")
		d.ProductSummary = d.RecentCommits[0]
		changes := max(0, len(strings.Split(status, "\n"))-1)
		if changes > 0 {
			d.ProductSummary = fmt.Sprintf("%d changed paths | %s", changes, d.ProductSummary)
		}
		d.Product = "WORKING TREE\n\n" + status + "\n\nRECENT COMMITS\n\n" + commits + "\n\nEnter: inspect tracked changes (staged and unstaged)."
		d.Diff = "UNSTAGED CHANGES\n\n" + gitText(root, "diff", "--stat") + "\n" + gitText(root, "diff", "--no-ext-diff", "--no-color") + "\n\nSTAGED CHANGES\n\n" + gitText(root, "diff", "--cached", "--no-ext-diff", "--no-color")
	} else {
		d.Product = "No company yet. Press c to set it up."
	}
	for _, a := range d.View.Agents {
		dir := filepath.Join(root, space.SpacesDir, a.Name)
		inbox := strings.Builder{}
		entries, err := os.ReadDir(filepath.Join(dir, "inbox"))
		if err != nil && !os.IsNotExist(err) {
			inbox.WriteString(err.Error())
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			d.Inboxes[a.Name] = append(d.Inboxes[a.Name], inboxRequest{entry.Name(), readDocument(filepath.Join(dir, "inbox", entry.Name(), "message.md"), false)})
		}
		if inbox.Len() == 0 {
			inbox.WriteString("Inbox is empty.")
		}
		output := a.Output
		if output == "" {
			output = "No captured terminal output."
		}
		if a.Error != "" {
			output = "ERROR: " + a.Error + "\n\n" + output
		}
		d.AgentDocs[a.Name] = []string{output, inbox.String(), readDocument(filepath.Join(dir, "notes.md"), false), readDocument(filepath.Join(dir, "goals.md"), false), readDocument(filepath.Join(dir, "role.md"), false)}
	}
	for _, r := range d.View.Runs {
		dir := filepath.Join(root, space.PublicDir, r.Name)
		var b strings.Builder
		fmt.Fprintf(&b, "%s / %s\n", r.Name, r.State)
		for _, file := range []string{"instructions.md", "version.txt", "impressions.md", "abandoned.txt"} {
			if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
				fmt.Fprintf(&b, "\n%s\n\n%s\n", file, readDocument(filepath.Join(dir, file), false))
			}
		}
		d.RunDocs[r.Name] = b.String()
	}
	return d
}

// durationSteps is the ladder Left/Right walk for interval fields.
var durationSteps = []string{"500ms", "1s", "2s", "3s", "5s", "10s", "15s", "20s", "30s", "45s", "1m", "2m", "5m", "10m"}

func durationOptions() []option {
	out := []option{}
	for _, s := range durationSteps {
		out = append(out, option{Value: s})
	}
	return out
}
func harnessOptions(c config.Config, inherit string) []option {
	names := []string{}
	for name := range c.Harnesses {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []option{}
	if inherit != "" {
		out = append(out, option{Value: "", Title: inherit})
	}
	for _, name := range names {
		out = append(out, option{Value: name})
	}
	return out
}
func valueOptions(values []string, empty string) []option {
	out := []option{{Value: "", Title: empty}}
	for _, v := range values {
		out = append(out, option{Value: v})
	}
	return out
}
func positionOptions(positions []bootstrap.Position) []option {
	out := []option{}
	for _, p := range positions {
		out = append(out, option{Value: p.Name, Title: p.Title + " · " + p.Sector})
	}
	return out
}
func settingsForm(root string) (*form, error) {
	c, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	h := c.Harnesses[c.Harness]
	userHarness := c.Harnesses[c.HarnessFor("")]
	fields := []field{
		{Label: "Goal", Value: c.Goal, Empty: "required unless a goal file is set"},
		{Label: "Goal file", Value: c.GoalFile, Kind: kindFile, Base: root, Empty: "none; relative to the company or absolute"},
		{Label: "Initial roster", Value: strings.Join(c.Roster, ", "), Kind: kindMulti, Options: positionOptions(bootstrap.Load(root).Positions()), Empty: "none"},
		{Label: "Default harness", Value: c.Harness, Kind: kindChoice, Options: harnessOptions(c, "")},
		{Label: "Model", Value: h.Model, Kind: kindChoice, Options: valueOptions(h.Models, "harness default")},
		{Label: "Thinking effort", Value: h.Effort, Kind: kindChoice, Options: valueOptions(h.Efforts, "harness default")},
		{Label: "Engine tick", Value: c.Tick.String(), Kind: kindDuration, Options: durationOptions()},
		{Label: "Dashboard refresh", Value: c.TUIRefresh.String(), Kind: kindDuration, Options: durationOptions()},
		{Label: "Session prefix", Value: c.SessionPrefix},
		{Label: "CEO instructions file", Value: c.CEOInstructionsFile, Kind: kindFile, Base: root, Empty: "none"},
		{Label: "Public tester harness", Value: c.User.Harness, Kind: kindChoice, Options: harnessOptions(c, "company default"), Empty: "company default"},
		{Label: "Public tester model", Value: c.User.Model, Kind: kindChoice, Options: valueOptions(userHarness.Models, "selected harness default"), Empty: "selected harness default"},
		{Label: "Public tester effort", Value: c.User.Effort, Kind: kindChoice, Options: valueOptions(userHarness.Efforts, "selected harness default"), Empty: "selected harness default"},
	}
	if c.GoalFile != "" {
		fields[0].Value = ""
	}
	// Model and effort belong to the selected harness; switching it swaps them.
	changed := func(f *form, i int) {
		if i == 3 {
			h := c.Harnesses[f.Fields[3].Value]
			f.Fields[4].Value, f.Fields[4].Options = h.Model, valueOptions(h.Models, "harness default")
			f.Fields[5].Value, f.Fields[5].Options = h.Effort, valueOptions(h.Efforts, "harness default")
		}
		if i == 10 || i == 3 && f.Fields[10].Value == "" {
			name := f.Fields[10].Value
			if name == "" {
				name = f.Fields[3].Value
			}
			h := c.Harnesses[name]
			f.Fields[11].Options = valueOptions(h.Models, "selected harness default")
			f.Fields[12].Options = valueOptions(h.Efforts, "selected harness default")
			f.Fields[11].Value, f.Fields[12].Value = "", ""
		}
	}
	return newForm("save-settings", "Company settings", fields, changed), nil
}
func saveSettings(root string, values []string) error {
	if len(values) != 13 {
		return fmt.Errorf("incomplete settings form")
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if strings.TrimSpace(values[1]) != "" && strings.TrimSpace(values[0]) != "" {
		return fmt.Errorf("use either Goal or Goal file; clear the other field with Ctrl-U")
	}
	overrides := []config.Override{}
	goalKey, goalValue := "goal", values[0]
	if strings.TrimSpace(values[1]) != "" {
		goalKey, goalValue = "goal_file", values[1]
	}
	overrides = append(overrides, config.Override{Key: goalKey, Value: goalValue})
	keys := []string{"roster", "harness", "model", "effort", "tick", "tui_refresh", "session_prefix", "ceo_instructions_file"}
	for i, k := range keys {
		section := ""
		if k == "model" || k == "effort" {
			section = "harness " + values[3]
		}
		overrides = append(overrides, config.Override{Section: section, Key: k, Value: values[i+2]})
	}
	for i, k := range []string{"harness", "model", "effort"} {
		overrides = append(overrides, config.Override{Section: "user", Key: k, Value: values[i+10]})
	}
	// Validate a candidate in memory before writing settings or creating output.
	for _, o := range overrides {
		if strings.ContainsAny(o.Value, "\r\n") {
			return fmt.Errorf("%s must fit on one line; use a file for multiline goals or CEO instructions", o.Key)
		}
		header := ""
		if o.Section != "" {
			header = "[" + o.Section + "]\n"
		}
		if err := cfg.Merge(header + o.Key + " = " + o.Value + "\n"); err != nil {
			return err
		}
	}
	if cfg.GoalFile != "" {
		p := cfg.GoalFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		cfg.Goal = string(b)
	}
	if err := bootstrap.Validate(root, cfg); err != nil {
		return err
	}
	for _, name := range cfg.Roster {
		if _, err := cfg.CommandFor(name, false); err != nil {
			return err
		}
	}
	if _, err := cfg.CommandFor("", false); err != nil {
		return fmt.Errorf("public tester: %w", err)
	}
	original, err := settingsForm(root)
	if err != nil {
		return err
	}
	changed := []config.Override{}
	for i, o := range overrides {
		fieldIndex := i + 1
		if i == 0 {
			if values[0] != original.Fields[0].Value || values[1] != original.Fields[1].Value {
				changed = append(changed, o)
			}
			continue
		}
		if values[fieldIndex] != original.Fields[fieldIndex].Value || strings.HasPrefix(o.Section, "harness ") && values[3] != original.Fields[3].Value {
			changed = append(changed, o)
		}
	}
	if len(changed) > 0 {
		if err := config.UpdateLocal(root, changed); err != nil {
			return err
		}
	}
	if bootstrap.Exists(root) {
		if _, err := bootstrap.SyncGoal(root, cfg); err != nil {
			return err
		}
		_, err = bootstrap.EnsureLayout(root)
		return err
	}
	return bootstrap.Init(root, cfg)
}
func command(root string, args ...string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	args = append(args, "-root", root, "--plain")
	c := exec.Command(exe, args...)
	b, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

func roleSettingsForm(root, name string) (*form, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	r := cfg.Roles[name]
	num := func(n int) string {
		if n == 0 {
			return ""
		}
		return fmt.Sprint(n)
	}
	suggestions := func(harness string) (models, efforts []option) {
		h := cfg.Harnesses[firstNonEmpty(harness, cfg.Harness)]
		return valueOptions(h.Models, "inherit"), valueOptions(h.Efforts, "inherit")
	}
	models, efforts := suggestions(r.Harness)
	fields := []field{
		{Label: "Employee", Value: name, Kind: kindStatic},
		{Label: "Harness", Value: r.Harness, Kind: kindChoice, Options: harnessOptions(cfg, "inherit "+cfg.Harness)},
		{Label: "Model", Value: r.Model, Kind: kindChoice, Options: models},
		{Label: "Thinking effort", Value: r.Effort, Kind: kindChoice, Options: efforts},
		{Label: "Idle ticks with inbox", Value: num(r.IdleTicks), Kind: kindNumber, Empty: fmt.Sprintf("inherit %d", cfg.IdleTicks)},
		{Label: "Idle ticks with empty inbox", Value: num(r.IdleTicksEmpty), Kind: kindNumber, Empty: fmt.Sprintf("inherit %d", cfg.IdleTicksEmpty)},
	}
	changed := func(f *form, i int) {
		if i == 1 {
			f.Fields[2].Options, f.Fields[3].Options = suggestions(f.Fields[1].Value)
		}
	}
	return newForm("role-settings", name+" · settings", fields, changed), nil
}
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// hireForm suggests the next free name for the profession, and follows the
// profession until the name is edited by hand.
func hireForm(positions []bootstrap.Position, agents []engine.AgentView, position, name string) *form {
	taken := map[string]bool{}
	for _, a := range agents {
		taken[a.Name] = true
	}
	suggest := func(position string) string {
		if !taken[position] {
			return position
		}
		for n := 2; ; n++ {
			if s := fmt.Sprintf("%s-%d", position, n); !taken[s] {
				return s
			}
		}
	}
	options := []option{}
	for _, p := range positions {
		if p.Name != "ceo" {
			options = append(options, option{Value: p.Name, Title: p.Title})
		}
	}
	auto := name == ""
	if auto {
		name = suggest(position)
	}
	fields := []field{
		{Label: "Name", Value: name},
		{Label: "Profession", Value: position, Kind: kindChoice, Options: options},
		{Label: "Backstory", Empty: "optional; drawn from the pool"},
	}
	changed := func(f *form, i int) {
		switch i {
		case 0:
			auto = false
		case 1:
			if auto {
				f.Fields[0].Value = suggest(f.Fields[1].Value)
			}
		}
	}
	return newForm("hire", "Hire an employee", fields, changed)
}
func saveRoleSettings(root string, values []string) error {
	if len(values) != 6 {
		return fmt.Errorf("incomplete employee settings")
	}
	name := values[0]
	if err := bootstrap.RoleName(name); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, space.SpacesDir, name)); err != nil {
		return fmt.Errorf("employee %s does not exist", name)
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	keys := []string{"harness", "model", "effort", "idle_ticks", "idle_ticks_empty"}
	updates := []config.Override{}
	for i, k := range keys {
		value := values[i+1]
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s must fit on one line", k)
		}
		if strings.HasPrefix(k, "idle_") && value == "" {
			value = "0"
		}
		o := config.Override{Section: "role " + name, Key: k, Value: value}
		if err := cfg.Merge("[" + o.Section + "]\n" + k + " = " + value + "\n"); err != nil {
			return err
		}
		updates = append(updates, o)
	}
	if _, err := cfg.CommandFor(name, false); err != nil {
		return err
	}
	return config.UpdateLocal(root, updates)
}
