package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vcomp/internal/bootstrap"
	"vcomp/internal/config"
)

func scopeField(scope string) field {
	return field{Label: "Scope", Value: scope, Kind: kindChoice, Options: []option{{Value: "global", Title: "My defaults (all companies)"}, {Value: "company", Title: "This company only"}}}
}
func (a *app) professionAction(act action) error {
	root := a.m.Root
	p := a.m.profession()
	scope := "global"
	if strings.HasPrefix(p.Source, config.LocalDir(root)+string(os.PathSeparator)) {
		scope = "company"
	}
	switch act.Kind {
	case "profession-new-form", "profession-generate-form":
		name, kind, brief := "", kindText, ""
		if act.Kind == "profession-generate-form" {
			if p.Name == "" {
				return fmt.Errorf("select a profession")
			}
			name = p.Name
			kind = kindStatic
		}
		a.m.Form = newForm("profession-prepare", "New profession draft", []field{{Label: "Profession slug", Value: name, Kind: kind}, {Label: "Description", Value: brief}, scopeField(scope), {Label: "Method", Value: "generate", Kind: kindChoice, Options: []option{{Value: "generate", Title: "Generate with configured model"}, {Value: "manual", Title: "Write manually"}}}, {Label: "Operation", Value: map[bool]string{true: "update", false: "create"}[name != ""], Kind: kindStatic}}, nil)
	case "profession-edit-form", "profession-delete-form", "profession-restore-form":
		if p.Name == "" {
			return fmt.Errorf("select a profession")
		}
		kind, title := "profession-edit", "Edit profession"
		if act.Kind == "profession-delete-form" {
			kind, title = "profession-delete-confirm", "Delete from catalogue"
		}
		if act.Kind == "profession-restore-form" {
			kind, title = "profession-restore", "Restore profession"
		}
		a.m.Form = newForm(kind, title, []field{{Label: "Profession", Value: p.Name, Kind: kindStatic}, scopeField(scope)}, nil)
	case "profession-edit":
		name, scope := act.Values[0], act.Values[1]
		text, err := bootstrap.PositionSet(root, scope).PositionText(name)
		if err != nil {
			return err
		}
		a.m.Draft = &professionDraft{Name: name, Scope: scope, Text: text}
		a.m.Form = nil
		a.m.Detail = "profession-draft"
		a.m.Scroll = 0
		return a.editProfessionDraft()
	case "profession-prepare":
		name, brief, scope, method, operation := act.Values[0], act.Values[1], act.Values[2], act.Values[3], act.Values[4]
		if err := bootstrap.PositionName(name); err != nil {
			return err
		}
		if _, err := bootstrap.TemplateDir(root, scope); err != nil {
			return err
		}
		if _, err := bootstrap.PositionSet(root, scope).PositionText(name); err == nil && operation == "create" {
			return fmt.Errorf("profession %s exists; edit it or choose another name", name)
		}
		draft := &professionDraft{Name: name, Scope: scope, Create: operation == "create"}
		if method == "manual" {
			text, err := bootstrap.Load(root).Text("role_blank.md", map[string]string{"TITLE": strings.ReplaceAll(name, "-", " "), "BRIEF": brief})
			if err != nil {
				return err
			}
			draft.Text = text
			a.m.Draft = draft
			a.m.Form = nil
			a.m.Detail = "profession-draft"
			a.m.Scroll = 0
			return a.editProfessionDraft()
		}
		if strings.TrimSpace(brief) == "" {
			return fmt.Errorf("describe the profession before generating")
		}
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.generationCancel = cancel
		a.m.Busy = true
		a.m.Message = "Generating draft with " + cfg.RoleGenerator.Model + "..."
		go func() {
			defer cancel()
			text, err := bootstrap.GeneratePosition(ctx, root, name, brief, cfg)
			draft.Text = text
			if text == "" {
				a.jobs <- result{Kind: "profession-generated", Err: err}
				return
			}
			a.jobs <- result{Kind: "profession-generated", Draft: draft, Err: err, Text: "Draft ready. Review, edit with e, then save with s."}
		}()
	case "profession-draft-edit":
		return a.editProfessionDraft()
	case "profession-save-confirm":
		if a.m.Draft == nil {
			return fmt.Errorf("no draft")
		}
		d := a.m.Draft
		if err := bootstrap.PositionSet(root, d.Scope).ValidatePosition(d.Name, d.Text); err != nil {
			return err
		}
		a.m.Confirm = &action{Kind: "profession-save"}
		a.m.Confirmation = fmt.Sprintf("Save %s in %s scope? A changed definition replaces affected running employees on their next engine tick.", d.Name, d.Scope)
	case "profession-save":
		if a.m.Draft == nil {
			return fmt.Errorf("no draft")
		}
		d := *a.m.Draft
		a.work(act.Kind, func() (string, error) {
			if _, err := bootstrap.PositionSet(root, d.Scope).PositionText(d.Name); err == nil && d.Create {
				return "", fmt.Errorf("profession now exists; reopen it before replacing")
			}
			err := bootstrap.SavePosition(root, d.Scope, d.Name, d.Text)
			return "Profession saved in " + d.Scope + " scope.", err
		})
	case "profession-delete-confirm":
		a.m.Confirm = &action{Kind: "profession-delete", Values: act.Values}
		a.m.Confirmation = fmt.Sprintf("Delete %s from the %s catalogue? Existing employees keep their definition. You can restore it later.", act.Values[0], act.Values[1])
	case "profession-delete", "profession-restore":
		name, scope := act.Values[0], act.Values[1]
		a.work(act.Kind, func() (string, error) {
			if act.Kind == "profession-delete" {
				return "Profession deleted from catalogue; existing employees retained.", bootstrap.DeletePosition(root, scope, name)
			}
			return "Profession restored.", bootstrap.RestorePosition(root, scope, name)
		})
	case "generation-settings-form":
		f, err := generationSettingsForm(root)
		if err != nil {
			return err
		}
		a.m.Form = f
	case "generation-settings-save":
		err := saveGenerationSettings(root, act.Values)
		if err != nil {
			return err
		}
		a.m.Form = nil
		a.m.Message = "Role generation settings saved. Employee models are unchanged."
		a.reload()
	default:
		return fmt.Errorf("unknown profession action %s", act.Kind)
	}
	return nil
}

func (a *app) editProfessionDraft() error {
	if a.m.Draft == nil {
		return fmt.Errorf("no draft")
	}
	dir, err := os.MkdirTemp("", "vcomp-profession-edit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, a.m.Draft.Name+".md")
	if err = os.WriteFile(path, []byte(a.m.Draft.Text), 0600); err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	args := strings.Fields(editor)
	if len(args) == 0 {
		return fmt.Errorf("EDITOR is empty")
	}
	runErr := a.external(exec.Command(args[0], append(args[1:], path)...))
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	a.m.Draft.Text = string(text)
	a.m.Scroll = 0
	if runErr != nil {
		return runErr
	}
	return bootstrap.PositionSet(a.m.Root, a.m.Draft.Scope).ValidatePosition(a.m.Draft.Name, a.m.Draft.Text)
}
func generationSettingsForm(root string) (*form, error) {
	company, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	global := config.Default()
	b, err := os.ReadFile(filepath.Join(config.Home(), config.FileName))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err = global.Merge(string(b)); err != nil {
		return nil, err
	}
	c := global.RoleGenerator
	fields := []field{scopeField("global"), {Label: "Model", Value: c.Model}, {Label: "Effort", Value: c.Effort, Kind: kindChoice, Options: valueOptions([]string{"low", "medium", "high", "max"}, "")}, {Label: "Timeout", Value: c.Timeout.String(), Kind: kindDuration, Options: durationOptions()}, {Label: "CLI command", Value: strings.Join(c.Command, " ")}}
	changed := func(f *form, i int) {
		if i == 0 {
			c := global.RoleGenerator
			if f.Fields[0].Value == "company" {
				c = company.RoleGenerator
			}
			f.Fields[1].Value = c.Model
			f.Fields[2].Value = c.Effort
			f.Fields[3].Value = c.Timeout.String()
			f.Fields[4].Value = strings.Join(c.Command, " ")
		}
	}
	return newForm("generation-settings-save", "Role generation settings", fields, changed), nil
}
func saveGenerationSettings(root string, values []string) error {
	if len(values) != 5 {
		return fmt.Errorf("incomplete generation settings")
	}
	if _, err := bootstrap.TemplateDir(root, values[0]); err != nil {
		return err
	}
	var updates []config.Override
	c := config.Default()
	for i, key := range []string{"role_generation_model", "role_generation_effort", "role_generation_timeout", "role_generation_command"} {
		v := values[i+1]
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("%s must fit on one line", key)
		}
		if err := c.Merge(key + " = " + v + "\n"); err != nil {
			return err
		}
		updates = append(updates, config.Override{Key: key, Value: v})
	}
	if len(c.RoleGenerationCommand()) == 0 || c.RoleGenerator.Model == "" {
		return fmt.Errorf("model and CLI command are required")
	}
	if values[0] == "global" {
		return config.UpdateGlobal(updates)
	}
	return config.UpdateLocal(root, updates)
}
