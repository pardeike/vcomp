package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfIsValid(t *testing.T) {
	c := Default()
	if c.Tick == 0 || c.IdleTicks == 0 || c.SessionPrefix == "" {
		t.Fatalf("default config is missing basics: %+v", c)
	}
	if _, ok := c.Harnesses[c.Harness]; !ok {
		t.Fatalf("default harness %q is not defined", c.Harness)
	}
	for _, kind := range []string{PromptFresh, PromptBack, PromptNudge, PromptUser, PromptUserNudge} {
		if c.Prompts[kind] == "" {
			t.Errorf("prompt %q is empty", kind)
		}
		if strings.Contains(c.Prompts[kind], "\n") {
			t.Errorf("prompt %q must be one line", kind)
		}
	}
	if len(c.Roster) == 0 {
		t.Fatal("default roster is empty")
	}
}

func TestEmptyPlaceholdersTakeTheirFlagWithThem(t *testing.T) {
	const conf = `harness = h
[harness h]
start = agent --model {{model}} --config=effort={{effort}} --go
`
	c, err := Parse(conf)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing set: both the separate-flag form and the joined form disappear
	// completely rather than leaving an orphaned flag or an empty argument.
	got, err := c.CommandFor("nobody", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "agent --go"; strings.Join(got, " ") != want {
		t.Fatalf("got %q, want %q", strings.Join(got, " "), want)
	}

	withValues, _ := Parse(conf + "model = m5\neffort = high\n")
	got, err = withValues.CommandFor("nobody", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "agent --model m5 --config=effort=high --go"; strings.Join(got, " ") != want {
		t.Fatalf("got %q, want %q", strings.Join(got, " "), want)
	}
}

// The shipped harnesses must survive having no model and no effort set, which
// is how a fresh company starts.
func TestShippedHarnessesExpandCleanly(t *testing.T) {
	c := Default()
	for name := range c.Harnesses {
		sub, _ := Parse("harness = " + name + "\n")
		sub.Harnesses = c.Harnesses
		for _, resume := range []bool{false, true} {
			cmd, err := sub.CommandFor("", resume)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, a := range cmd {
				if strings.Contains(a, "{{") || strings.HasSuffix(a, "=") {
					t.Errorf("%s resume=%v: leftover argument %q in %v", name, resume, a, cmd)
				}
			}
		}
	}
}

// Local-model presets must keep unattended/resume flags while dropping optional
// model controls. A role's provider/model selector must reach either command.
func TestLocalModelHarnessCommands(t *testing.T) {
	for _, tc := range []struct {
		name, fresh, resumed, controls string
	}{
		{"pi", "pi", "pi --continue", " --model local/my-model --thinking low"},
		{"omp", "env OMP_SKIP_SETUP=1 omp --auto-approve", "env OMP_SKIP_SETUP=1 omp --continue --auto-approve", " --model local/my-model --thinking low"},
		{"opencode", "opencode --auto", "opencode --continue --auto", " --model local/my-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			c.Harness = tc.name
			for _, configured := range []bool{false, true} {
				if configured {
					c.Roles["developer"] = Role{Model: "local/my-model", Effort: "low"}
				}
				for _, resume := range []bool{false, true} {
					want := tc.fresh
					if resume {
						want = tc.resumed
					}
					if configured {
						want += tc.controls
					}
					got, err := c.CommandFor("developer", resume)
					if err != nil || strings.Join(got, " ") != want {
						t.Fatalf("configured=%v resume=%v: got %v, %v; want %q", configured, resume, got, err, want)
					}
				}
			}
		})
	}
}

func TestRoleOverridesHarnessModelAndPrompt(t *testing.T) {
	c, err := Parse(`harness = a
[harness a]
start = a-cli
[harness b]
start = b-cli --model {{model}} --effort {{effort}}
model = default-model
effort = low
[prompts]
nudge = get on with it
[role ceo]
harness = b
model = fancy
nudge = go and ask someone a hard question
`)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := c.CommandFor("ceo", false)
	if err != nil {
		t.Fatal(err)
	}
	// model comes from the role, effort falls back to the harness default.
	if got, want := strings.Join(cmd, " "), "b-cli --model fancy --effort low"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, err := c.CommandFor("tester", false); err != nil || strings.Join(got, " ") != "a-cli" {
		t.Fatalf("other roles keep the default harness, got %v %v", got, err)
	}
	if got := c.Prompt("ceo", PromptNudge); got != "go and ask someone a hard question" {
		t.Fatalf("role prompt override ignored, got %q", got)
	}
	if got := c.Prompt("tester", PromptNudge); got != "get on with it" {
		t.Fatalf("other roles keep the default prompt, got %q", got)
	}
}

func TestValuesKeepSpacesEqualsAndHashes(t *testing.T) {
	c, err := Parse("[prompts]\nnudge = read #3 and set x = 1, then continue\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Prompts[PromptNudge]; got != "read #3 and set x = 1, then continue" {
		t.Fatalf("value was mangled: %q", got)
	}
}

func TestTyposAreRejected(t *testing.T) {
	for _, bad := range []string{
		"tikc = 20s\n",
		"tick = soon\n",
		"[nonsense]\nx = 1\n",
		"[harness]\nstart = x\n",
		"[prompts]\nfrsh = hello\n",
		"just a line\n",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("should have been rejected: %q", bad)
		}
	}
}

func TestResumeFallsBackToStart(t *testing.T) {
	c, _ := Parse("harness = h\n[harness h]\nstart = only-one\n")
	got, err := c.CommandFor("", true)
	if err != nil || strings.Join(got, " ") != "only-one" {
		t.Fatalf("a harness without resume should reuse start, got %v %v", got, err)
	}
}

func TestPublicTesterOverridesAndInheritance(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())
	root := t.TempDir()
	global := `harness = omp
[harness omp]
model = ollama/weak
effort = high
[harness codex]
model = frontier-default
effort = low
handshake = Enter
[user]
harness = codex
model = frontier-reviewer
effort = high
[role user]
model = ordinary-employee
`
	if err := os.WriteFile(filepath.Join(Home(), FileName), []byte(global), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err := c.CommandFor("", false)
	if err != nil || strings.Join(cmd, " ") != "codex --dangerously-bypass-approvals-and-sandbox --model=frontier-reviewer --config=model_reasoning_effort=high" {
		t.Fatalf("public tester command: %v, %v", cmd, err)
	}
	if strings.Join(c.Handshake(""), " ") != "Enter" || c.HarnessFor("") != "codex" {
		t.Fatal("public tester did not select its harness and handshake")
	}
	for _, role := range []string{"ceo", "developer-1", "future-hire"} {
		cmd, err := c.CommandFor(role, false)
		if err != nil || !strings.Contains(strings.Join(cmd, " "), "--model ollama/weak --thinking high") {
			t.Fatalf("employee %s inherited reviewer settings: %v, %v", role, cmd, err)
		}
	}
	if c.User.Model == c.Roles["user"].Model {
		t.Fatal("[role user] overwrote [user]")
	}
	// Blank per-company model/effort inherit the selected review harness.
	if err := UpdateLocal(root, []Override{{Section: "user", Key: "model"}, {Section: "user", Key: "effort"}}); err != nil {
		t.Fatal(err)
	}
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd, err = c.CommandFor("", false)
	if err != nil || !strings.Contains(strings.Join(cmd, " "), "--model=frontier-default --config=model_reasoning_effort=low") {
		t.Fatalf("reviewer inheritance: %v, %v", cmd, err)
	}
	if err := UpdateLocal(root, []Override{{Section: "user", Key: "harness"}}); err != nil {
		t.Fatal(err)
	}
	c, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	user, err := c.CommandFor("", false)
	worker, _ := c.CommandFor("ceo", false)
	if err != nil || strings.Join(user, " ") != strings.Join(worker, " ") {
		t.Fatalf("empty [user] should preserve company defaults: %v, %v", user, err)
	}
	for _, bad := range []string{"[user named]\nmodel = x", "[user]\nmodle = x", "[user]\nstart = x"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("accepted invalid configuration %q", bad)
		}
	}
	c.User.Harness = "missing"
	if _, err := c.CommandFor("", false); err == nil {
		t.Fatal("unknown public harness silently fell back to company model")
	}
}

func TestUpdateLocalKeepsUnansweredSettings(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())
	root := t.TempDir()
	os.MkdirAll(LocalDir(root), 0755)
	os.WriteFile(filepath.Join(LocalDir(root), FileName), []byte("goal = retained\nroster = ceo\nsession_prefix = custom\n[role ceo]\nmodel = special\n"), 0644)
	if err := UpdateLocal(root, []Override{{Key: "tick", Value: "2s"}}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Goal != "retained" || c.SessionPrefix != "custom" || c.Roles["ceo"].Model != "special" || len(c.Roster) != 1 {
		t.Fatalf("settings lost: %+v", c)
	}
}

func TestLaunchRetryDelay(t *testing.T) {
	if Default().LaunchRetryDelay <= 0 {
		t.Fatal("missing launch retry delay default")
	}
	for _, value := range []string{"0s", "-1s", "invalid"} {
		if _, err := Parse("launch_retry_delay = " + value); err == nil {
			t.Fatalf("accepted invalid retry delay %q", value)
		}
	}
	c, err := Parse("launch_retry_delay = 7s")
	if err != nil || c.LaunchRetryDelay.String() != "7s" {
		t.Fatalf("retry delay override: %v, %v", c.LaunchRetryDelay, err)
	}
}

func TestOptionalMCPSettings(t *testing.T) {
	cfg := Default()
	if !cfg.MCPEnabled || cfg.MCPTimeout != 30*time.Second {
		t.Fatal("missing MCP defaults")
	}
	if err := cfg.setTop("mcp_enabled", "false"); err != nil || cfg.MCPEnabled {
		t.Fatal("cannot disable MCP", err)
	}
	if err := cfg.setTop("mcp_timeout", "2s"); err != nil || cfg.MCPTimeout != 2*time.Second {
		t.Fatal("cannot set MCP deadline", err)
	}
	if err := cfg.setTop("mcp_enabled", "perhaps"); err == nil {
		t.Fatal("accepted invalid boolean")
	}
	if err := cfg.setTop("mcp_timeout", "0s"); err == nil {
		t.Fatal("accepted zero deadline")
	}
}
