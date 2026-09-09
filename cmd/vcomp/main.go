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
)

const usage = `vcomp - a virtual company of AI agents

  vcomp install  [-force]               write the defaults to ~/.vcomp/
  vcomp setup    [-root DIR]            ask for settings, save only what differs
  vcomp run      [-root DIR] [-goal ..] keep the company alive (foreground)
  vcomp status   [-root DIR]
  vcomp user-run [-root DIR] [-instructions FILE] [-text "..."]
  vcomp attach   [-root DIR] ROLE       watch someone work
  vcomp stop     [-root DIR]            kill every session

An empty directory is a valid company: "vcomp run -goal ..." fills it in.
Settings resolve built-in defaults, then ~/.vcomp/, then DIR/.vcomp/.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "install":
		err = cmdInstall(args)
	case "setup":
		err = cmdSetup(args)
	case "run":
		err = cmdRun(args)
	case "status":
		err = cmdStatus(args)
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
	if err != nil {
		fmt.Fprintln(os.Stderr, "vcomp:", err)
		os.Exit(1)
	}
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

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	root, err := company(fs, args)
	if err != nil {
		return err
	}
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

	local := config.LocalDir(root)
	if err := os.MkdirAll(local, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(local, config.FileName)
	if len(overrides) == 0 {
		fmt.Printf("\nNothing differs from the inherited settings; no %s written.\n", dest)
	} else {
		if err := os.WriteFile(dest, []byte(config.RenderOverrides(overrides)), 0o644); err != nil {
			return err
		}
		fmt.Printf("\n%d settings written to %s\n", len(overrides), dest)
	}

	if bootstrap.Exists(root) {
		fmt.Println("company already exists; nothing else to do")
		return nil
	}
	if cfg, err = config.Load(root); err != nil {
		return err
	}
	cfg.Goal = goal
	if err := bootstrap.Init(root, cfg); err != nil {
		return err
	}
	fmt.Printf("company created; start it with: vcomp run -root %s\n", root)
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

	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; close(stop) }()

	if !e.Run(stop) {
		return nil
	}
	result, _ := e.Result()
	fmt.Printf("\n%s\n", strings.TrimSpace(result))
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
	return nil
}

func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()
	fmt.Printf("killed %d sessions\n", e.Stop())
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
	root, err := company(fs, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: vcomp attach [-root DIR] ROLE")
	}
	e, err := engine.New(root)
	if err != nil {
		return err
	}
	defer e.Close()
	session := e.Session(fs.Arg(0))
	if !tmux.Exists(session) {
		return fmt.Errorf("no session %q (try: vcomp status)", session)
	}
	c := exec.Command("tmux", "attach", "-t", session)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
