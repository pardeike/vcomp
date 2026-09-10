// Command vcomp runs a virtual company of AI agents. See AGENTS.md.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
	"vcomp/internal/engine"
	"vcomp/internal/space"
	"vcomp/internal/tmux"
	"vcomp/internal/tui"
)

const usage = `vcomp - a virtual company of AI agents

  vcomp                                 open the interactive dashboard
  vcomp tui      [-root DIR]            open the terminal interface
  vcomp start    [-root DIR]            start supervision with the dashboard
  vcomp install  [-force]               write the defaults to ~/.vcomp/
  vcomp setup    [-root DIR]            ask for settings, save only what differs
  vcomp run      [-root DIR] [-goal ..] keep the company alive (foreground)
  vcomp roles    [-root DIR]            list the role names you can put in a roster
  vcomp hire     NAME [-position P] [-backstory "..."] [-replace]
  vcomp steer    NAME [-root DIR] [-text "..."] [-file FILE]
  vcomp status   [-root DIR]
  vcomp reset    [-root DIR] [-y]       start over, keeping the settings
  vcomp user-run [-root DIR] [-instructions FILE] [-text "..."]
  vcomp attach   [-root DIR] ROLE       watch someone work
  vcomp stop     [-root DIR] [-all]     kill this company's sessions, or every one

Any empty directory is a company waiting to happen:

  mkdir /tmp/vgame && cd /tmp/vgame && vcomp

Interactive commands open the relevant TUI screen. --plain keeps the CLI.
Redirected input/output also uses the CLI. Fully specified mutation commands
execute directly. Settings resolve built-in defaults, ~/.vcomp/, then DIR/.vcomp/.
`

func main() {
	// Bare "vcomp" is the whole point: cd somewhere and get going.
	cmd, args := "start", []string{}
	if len(os.Args) > 1 {
		cmd, args = os.Args[1], os.Args[2:]
	}
	if strings.HasPrefix(cmd, "-") && cmd != "-h" && cmd != "--help" {
		cmd, args = "start", os.Args[1:]
	}
	var err error
	args, plain := plainArgs(args)
	if !plain && (cmd == "tui" || tui.Interactive() && wantsTUI(cmd, args)) {
		uiCommand := cmd
		if len(os.Args) == 1 {
			uiCommand = "tui"
		}
		err = cmdTUI(uiCommand, args)
	} else {
		switch cmd {
		case "start":
			err = cmdStart(args)
		case "install":
			err = cmdInstall(args)
		case "setup":
			err = cmdSetup(args)
		case "run":
			err = cmdRun(args)
		case "roles":
			err = cmdRoles(args)
		case "hire":
			err = cmdHire(args)
		case "steer":
			err = cmdSteer(args)
		case "status":
			err = cmdStatus(args)
		case "reset":
			err = cmdReset(args)
		case "stop":
			err = cmdStop(args)
		case "user-run":
			err = cmdUserRun(args)
		case "attach":
			err = cmdAttach(args)
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		default:
			err = fmt.Errorf("unknown command %q", cmd)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vcomp:", err)
		os.Exit(1)
	}
}

// nameAndFlags parses "cmd [flags] NAME [flags]". Go's flag package stops at
// the first non-flag argument, and insisting that flags come first is not how
// anyone types - least of all an agent following an example.
func nameAndFlags(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return "", nil
	}
	name := rest[0]
	if err := fs.Parse(rest[1:]); err != nil {
		return "", err
	}
	if fs.NArg() != 0 {
		return "", fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return name, nil
}

// company parses the shared -root flag and resolves it to an absolute path.
func company(fs *flag.FlagSet, args []string) (string, error) {
	root := fs.String("root", ".", "company root directory")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	return filepath.Abs(*root)
}

func cmdInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	force := fs.Bool("force", false, "overwrite files that already exist")
	fs.Parse(args)

	home := config.Home()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	n := 0
	conf := filepath.Join(home, config.FileName)
	if _, err := os.Stat(conf); err != nil || *force {
		if err := os.WriteFile(conf, []byte(config.DefaultText()), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", conf)
		n++
	}
	written, err := bootstrap.Export(filepath.Join(home, config.TemplatesDir), *force)
	if err != nil {
		return err
	}
	n += len(written)
	fmt.Printf("%d files installed in %s\n", n, home)
	if n == 0 {
		fmt.Println("everything was already there; use -force to overwrite")
	}
	return nil
}

// cmdStart is what plain "vcomp" does: set this directory up if it is not a
// company yet, then keep it running.
func cmdStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	if !bootstrap.Exists(root) {
		if err := setupCompany(root); err != nil {
			return err
		}
		fmt.Println()
	}
	return runEngine(root)
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	return setupCompany(root)
}

// setupCompany asks for every setting, keeps only the answers that differ from
// what is inherited, and creates the company if it is not there yet.
func setupCompany(root string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	in := bufio.NewScanner(os.Stdin)

	fmt.Printf("Setting up a company in %s\n", root)
	fmt.Printf("Press Enter to keep the inherited value shown in brackets.\n\n")

	var overrides []config.Override
	ask := func(section, key, current string) string {
		label := key
		if section != "" {
			label = section + " " + key
		}
		fmt.Printf("%-18s [%s]: ", label, current)
		if !in.Scan() {
			return ""
		}
		answer := strings.TrimSpace(in.Text())
		if answer == "" {
			return current
		}
		if err := config.Valid(sectionKind(section), sectionName(section), key, answer); err != nil {
			fmt.Printf("   ignored: %v\n", err)
			return current
		}
		overrides = append(overrides, config.Override{Section: section, Key: key, Value: answer})
		return answer
	}

	goal := ask("", "goal", cfg.Goal)
	ask("", "roster", strings.Join(cfg.Roster, ", "))
	harness := ask("", "harness", cfg.Harness)
	h := cfg.Harnesses[harness]
	ask("harness "+harness, "model", h.Model)
	ask("harness "+harness, "effort", h.Effort)
	ask("", "tick", cfg.Tick.String())
	ask("", "idle_ticks", fmt.Sprint(cfg.IdleTicks))
	ask("", "idle_ticks_empty", fmt.Sprint(cfg.IdleTicksEmpty))
	ask("", "user_timeout", cfg.UserTimeout.String())
	ask("", "user_max_attempts", fmt.Sprint(cfg.UserMaxAttempts))
	ask("", "session_prefix", cfg.SessionPrefix)
	ask("", "result_file", cfg.ResultFile)

	fmt.Printf("\nPer-role overrides. Enter a role name, or Enter to finish.\n")
	for {
		fmt.Printf("%-18s [ ]: ", "role")
		if !in.Scan() {
			break
		}
		name := strings.TrimSpace(in.Text())
		if name == "" {
			break
		}
		r := cfg.Roles[name]
		ask("role "+name, "harness", r.Harness)
		ask("role "+name, "model", r.Model)
		ask("role "+name, "effort", r.Effort)
		ask("role "+name, "nudge", r.Prompts[config.PromptNudge])
	}

	// Nothing has been written yet, so an aborted setup leaves no trace.
	exists := bootstrap.Exists(root)
	if !exists && strings.TrimSpace(goal) == "" {
		return fmt.Errorf("no goal given, so nothing was created in %s", root)
	}

	local := config.LocalDir(root)
	if err := os.MkdirAll(local, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(local, config.FileName)
	if len(overrides) == 0 {
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("\nNo answers changed, so %s is left as it is.\n", dest)
		} else {
			fmt.Printf("\nNo answers differ from the inherited settings, so no %s was written.\n", dest)
		}
	} else {
		if err := config.UpdateLocal(root, overrides); err != nil {
			return err
		}
		fmt.Printf("\n%d settings written to %s\n", len(overrides), dest)
	}

	if cfg, err = config.Load(root); err != nil {
		return err
	}
	cfg.Goal = goal
	if exists {
		changed, err := bootstrap.SyncGoal(root, cfg)
		if err != nil {
			return err
		}
		if changed {
			fmt.Println("the CEO has been given the new goal")
		}
		made, err := bootstrap.EnsureLayout(root)
		if err != nil {
			return err
		}
		if len(made) > 0 {
			fmt.Printf("restored missing directories: %s\n", strings.Join(made, ", "))
		}
		fmt.Printf("\n%s\n", bootstrap.Describe(root))
		return nil
	}
	if err := bootstrap.Init(root, cfg); err != nil {
		return err
	}
	fmt.Printf("\n%s\n", bootstrap.Describe(root))
	return nil
}

// sectionKind and sectionName split "role ceo" into its two halves.
func sectionKind(section string) string {
	kind, _, _ := strings.Cut(section, " ")
	return kind
}

func sectionName(section string) string {
	_, name, _ := strings.Cut(section, " ")
	return name
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	goal := fs.String("goal", "", "the goal, if the company does not exist yet")
	goalFile := fs.String("goal-file", "", "read the goal from a file instead")
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	if !tmux.Available() {
		return fmt.Errorf("tmux is not installed")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	if !bootstrap.Exists(root) {
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		if *goalFile != "" {
			b, err := os.ReadFile(*goalFile)
			if err != nil {
				return err
			}
			cfg.Goal = string(b)
		}
		if *goal != "" {
			cfg.Goal = *goal
		}
		if err := bootstrap.Init(root, cfg); err != nil {
			return err
		}
		fmt.Printf("created a company in %s: %s\n", root, strings.Join(cfg.Roster, ", "))
	}

	return runEngine(root)
}

// runEngine keeps the company alive until it is interrupted, or until the CEO
// declares the goal reached - in which case its answer is what you get back.
func runEngine(root string) error {
	if !tmux.Available() {
		return fmt.Errorf("tmux is not installed")
	}
	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()
	finished := make(chan struct{})
	defer close(finished)
	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		select {
		case <-sig:
			close(stop)
		case <-finished:
		}
	}()
	if err := e.Lock(); err != nil {
		return err
	}
	defer e.Unlock()

	// A result file that is already here was not written by this run's CEO, so
	// this company is finished rather than finishing.
	if result, done := e.Result(); done {
		fmt.Printf("%s already exists, so this company is already finished:\n\n%s\n\n",
			e.ResultFile(), strings.TrimSpace(result))
		return fmt.Errorf("remove %s to let this company carry on, or start a fresh one in another directory",
			filepath.Join(root, e.ResultFile()))
	}

	// A goal edited since the company was created has to reach the CEO.
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if changed, err := bootstrap.SyncGoal(root, cfg); err != nil {
		return err
	} else if changed {
		fmt.Println("the goal changed since this company was created; the CEO has been told")
	}

	if !e.Run(stop) {
		return nil
	}
	result, _ := e.Result()
	fmt.Printf("\n%s\n", strings.TrimSpace(result))
	return nil
}

// cmdRoles answers "what can I put in the roster", which is otherwise only
// discoverable by listing the templates directory.
func cmdRoles(args []string) error {
	fs := flag.NewFlagSet("roles", flag.ExitOnError)
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	set := bootstrap.Load(root)

	fmt.Printf("Role catalogue:\n")
	sector := ""
	for _, p := range set.Positions() {
		if p.Sector != sector {
			sector = p.Sector
			fmt.Printf("\n  %s\n", sector)
		}
		fmt.Printf("    %-22s %s\n", p.Name, p.Title)
	}
	fmt.Printf("\nNames ending in -N share a profession. Custom names need -position.\n" +
		"Each employee combines a fixed profession, a backstory and optional CEO steering.\n" +
		"The CEO's extra instructions come only from ceo_instructions_file.\n")
	return nil
}

// cmdHire composes a role from its two parts instead of having the document
// written freehand, which is what keeps invented roles behaving like roles.
func cmdHire(args []string) error {
	fs := flag.NewFlagSet("hire", flag.ExitOnError)
	dir := fs.String("root", ".", "company root directory")
	position := fs.String("position", "", "profession from the catalogue (default: guessed from the name)")
	backstory := fs.String("backstory", "", "this person's background (default: drawn from the pool)")
	replace := fs.Bool("replace", false, "overwrite an existing role, ending whoever is in it")
	name, err := nameAndFlags(fs, args)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf(`usage: vcomp hire NAME [-position P] [-backstory "..."] [-replace]`)
	}
	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if err := bootstrap.Hire(root, cfg, name, *position, *backstory, *replace); err != nil {
		return err
	}
	verb := "hired"
	if *replace {
		verb = "replaced"
	}
	fmt.Printf("%s %s; the engine will start them on its next tick\n", verb, name)
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()
	e.Status(os.Stdout)

	// Agents outlive their engine on purpose, which makes it easy to forget
	// they are there. Say so rather than leaving it to "tmux ls".
	if others := engine.Orphans(); len(others) > 0 {
		fmt.Printf("\nRUNNING SESSIONS ON THIS MACHINE\n")
		for _, s := range others {
			fmt.Printf("  %s\n", s)
		}
		fmt.Printf("\"vcomp stop -all\" ends all of them.\n")
	}
	return nil
}

// cmdReset throws the company away and builds it again from the same settings,
// which is the difference between starting over and starting from nothing.
func cmdReset(args []string) error {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	yes := fs.Bool("y", false, "do not ask for confirmation")
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	if !bootstrap.Exists(root) {
		return fmt.Errorf("no company in %s to reset", root)
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	cfg, err = bootstrap.RetainGoal(root, cfg)
	if err != nil {
		return err
	}
	if err := bootstrap.Validate(root, cfg); err != nil {
		return err
	}
	produced := bootstrap.Produced(root, cfg)
	fmt.Printf("This deletes everything the company produced in %s:\n", root)
	for _, p := range produced {
		fmt.Printf("  %s\n", strings.TrimPrefix(p, root+string(filepath.Separator)))
	}
	fmt.Printf("It keeps %s and any local templates, and builds the company again from them.\n",
		filepath.Join(config.LocalDir(root), config.FileName))

	if !*yes {
		fmt.Printf("\nType yes to go ahead: ")
		in := bufio.NewScanner(os.Stdin)
		if !in.Scan() || strings.TrimSpace(in.Text()) != "yes" {
			return fmt.Errorf("nothing was removed")
		}
	}

	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()
	if err := e.StopAndLock(); err != nil {
		return err
	}
	if _, err := e.Stop(); err != nil {
		return err
	}
	if err := bootstrap.Reset(root, cfg); err != nil {
		return err
	}
	fmt.Printf("\n%s\n", bootstrap.Describe(root))
	return nil
}

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	all := fs.Bool("all", false, "kill every vcomp session on this machine, whatever company")
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	roots := []string{root}
	if *all {
		roots = engine.CompanyRoots()
	}
	n := 0
	for _, companyRoot := range roots {
		e, err := engine.NewControl(companyRoot)
		if err != nil {
			return err
		}
		if err := e.StopAndLock(); err != nil {
			e.Close()
			return err
		}
		killed, stopErr := e.Stop()
		n += killed
		e.Close()
		if stopErr != nil {
			return stopErr
		}
	}
	fmt.Printf("killed %d sessions\n", n)
	return nil
}

func cmdUserRun(args []string) error {
	fs := flag.NewFlagSet("user-run", flag.ExitOnError)
	file := fs.String("instructions", "", "file to place as instructions.md")
	text := fs.String("text", "", "instructions text")
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	name := space.NextRunName(root)
	runDir := filepath.Join(root, space.PublicDir, name)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	body := *text
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		body = string(b)
	}
	if strings.TrimSpace(body) != "" {
		if err := os.WriteFile(filepath.Join(runDir, "instructions.md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("%s created; the engine will send in a user on its next tick\n", runDir)
	return nil
}

func cmdAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ExitOnError)
	dir := fs.String("root", ".", "company root directory")
	name, err := nameAndFlags(fs, args)
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("usage: vcomp attach ROLE [-root DIR]")
	}
	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()
	session := e.Session(name)
	if !tmux.Exists(session) {
		return fmt.Errorf("no session %q (try: vcomp status)", session)
	}
	c := exec.Command("tmux", "attach", "-t", session)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func cmdSteer(args []string) error {
	fs := flag.NewFlagSet("steer", flag.ContinueOnError)
	dir := fs.String("root", ".", "company root directory")
	text := fs.String("text", "", "CEO steering text; empty clears it")
	file := fs.String("file", "", "read steering text from a file")
	name, err := nameAndFlags(fs, args)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		*text = string(b)
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if err := bootstrap.Steer(root, cfg, name, *text); err != nil {
		return err
	}
	fmt.Printf("updated %s's steering; the profession and backstory are unchanged\n", name)
	return nil
}
