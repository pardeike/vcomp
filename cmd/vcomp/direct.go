package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"vcomp/internal/engine"
)

func cmdDirectSteer(args []string) error {
	fs := flag.NewFlagSet("direct-steer", flag.ContinueOnError)
	root := fs.String("root", ".", "company root")
	all := fs.Bool("all", false, "broadcast to all running employees, including the CEO")
	mode := fs.String("mode", "queued", "queued or immediate")
	text := fs.String("text", "", "prompt to send into the existing conversation")
	file := fs.String("file", "", "read prompt from a file")
	name, err := nameAndFlags(fs, args)
	if err != nil {
		return err
	}
	if *all == (name != "") {
		return fmt.Errorf("choose one employee or -all")
	}
	if *mode != "queued" && *mode != "immediate" {
		return fmt.Errorf("mode must be queued or immediate")
	}
	if *file != "" {
		if *text != "" {
			return fmt.Errorf("choose -text or -file")
		}
		b, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		*text = string(b)
	}
	if strings.TrimSpace(*text) == "" {
		return fmt.Errorf("provide a prompt with -text or -file")
	}
	results, err := engine.RequestDirect(*root, engine.DirectRequest{Role: name, All: *all, Mode: *mode, Text: *text})
	if err != nil {
		return err
	}
	failed := false
	for _, r := range results {
		fmt.Printf("%s: %s\n", r.Role, r.Status)
		failed = failed || strings.HasPrefix(r.Status, "failed:")
	}
	if failed {
		return fmt.Errorf("some prompts were not delivered; see per-employee results above")
	}
	return nil
}
