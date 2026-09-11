package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"vcomp/internal/bootstrap"
)

func cmdMessage(args []string) error {
	fs := flag.NewFlagSet("message", flag.ContinueOnError)
	dir := fs.String("root", ".", "company root directory")
	subject := fs.String("subject", "", "inbox request subject")
	text := fs.String("text", "", "message FROM USER")
	file := fs.String("file", "", "read message body from file")
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
	path, err := bootstrap.Message(root, name, *subject, *text)
	if err != nil {
		return err
	}
	fmt.Printf("Message delivered: %s\n", path)
	return nil
}
