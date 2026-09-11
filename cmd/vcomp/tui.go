package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"vcomp/internal/tui"
)

func plainArgs(args []string) ([]string, bool) {
	out := []string{}
	plain := false
	for _, arg := range args {
		if arg == "--plain" || arg == "-plain" {
			plain = true
		} else {
			out = append(out, arg)
		}
	}
	return out, plain
}

// Read/navigation commands and unfilled actions enter the TUI on a terminal.
// Fully specified mutation commands remain immediate, which also keeps CLI
// agents with a pseudo-terminal from unexpectedly entering a form.
func wantsTUI(cmd string, args []string) bool {
	switch cmd {
	case "start", "run", "status", "setup", "roles", "tui":
		return true
	case "hire", "steer", "message", "reset", "stop", "user-run":
		for i := 0; i < len(args); i++ {
			if args[i] == "-root" || args[i] == "--root" {
				i++
				continue
			}
			if strings.HasPrefix(args[i], "-") {
				return false
			}
		}
		return true
	}
	return false
}
func cmdTUI(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	root := fs.String("root", ".", "company root directory")
	goal := fs.String("goal", "", "company goal")
	goalFile := fs.String("goal-file", "", "read the goal from a file")
	name, err := nameAndFlags(fs, args)
	if err == flag.ErrHelp {
		return nil
	}
	if err != nil {
		return err
	}
	path, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	opt := tui.Options{Goal: *goal, GoalFile: *goalFile, Name: name}
	switch cmd {
	case "run", "start":
		opt.Start = true // explicit start/run supervises; bare vcomp opens the dashboard
	case "setup":
		opt.Screen = 3
	case "roles":
		opt.Screen = 6
	case "hire":
		opt.Action = "hire-form"
	case "message":
		opt.Action = "message-form"
	case "steer":
		opt.Action = "steer-form"
	case "user-run":
		opt.Screen = 1
		opt.Action = "test-form"
	case "reset":
		opt.Action = "reset-confirm"
	case "stop":
		opt.Action = "stop-confirm"
	}
	if name != "" && cmd != "hire" && cmd != "steer" && cmd != "message" {
		return fmt.Errorf("unexpected argument %q", name)
	}
	return tui.Run(path, opt)
}
