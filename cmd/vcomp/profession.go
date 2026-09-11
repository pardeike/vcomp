package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
)

func cmdProfession(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("use profession list, show, create, update, delete, restore, or generate")
	}
	operation := args[0]
	fs := flag.NewFlagSet("profession "+operation, flag.ContinueOnError)
	root := fs.String("root", ".", "company directory")
	scope := fs.String("scope", "company", "company or global")
	file := fs.String("file", "", "definition file for create/update")
	brief := fs.String("brief", "", "description for generation")
	name, err := nameAndFlags(fs, args[1:])
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if _, err = bootstrap.TemplateDir(abs, *scope); err != nil {
		return err
	}
	set := bootstrap.PositionSet(abs, *scope)
	if operation == "list" {
		for _, p := range set.Catalogue() {
			state := "active"
			if p.Deleted {
				state = "deleted"
			}
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", p.Name, p.Title, p.Sector, state, p.Source)
		}
		return nil
	}
	if err = bootstrap.PositionName(name); err != nil {
		return err
	}
	switch operation {
	case "show":
		text, err := set.PositionText(name)
		if err == nil {
			fmt.Print(text)
		}
		return err
	case "create", "update":
		_, exists := set.PositionText(name)
		if operation == "create" && exists == nil {
			return fmt.Errorf("profession %s already exists; use update or restore", name)
		}
		if operation == "update" && exists != nil {
			return exists
		}
		if *file == "" {
			return fmt.Errorf("provide -file with a definition to save")
		}
		text, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		return bootstrap.SavePosition(abs, *scope, name, string(text))
	case "delete":
		return bootstrap.DeletePosition(abs, *scope, name)
	case "restore":
		return bootstrap.RestorePosition(abs, *scope, name)
	case "generate":
		cfg, err := config.Load(abs)
		if err != nil {
			return err
		}
		text, err := bootstrap.GeneratePosition(context.Background(), abs, name, *brief, cfg)
		if text != "" {
			fmt.Print(text)
		}
		return err
	default:
		return fmt.Errorf("unknown profession operation %q", operation)
	}
}
