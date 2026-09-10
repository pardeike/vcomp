# vcomp

A virtual company of CLI coding agents. Each employee has a profession, a
backstory, a folder, and a persistent tmux conversation. They coordinate through
files while vcomp keeps their sessions running.

## Start a company

You need Go to build vcomp, plus `git`, `tmux`, and at least one configured agent
CLI: Claude Code, Codex, pi, Oh My Pi, or OpenCode.

```sh
./install.sh
mkdir /tmp/my-company
cd /tmp/my-company
vcomp
```

The terminal interface opens a setup form. Enter a goal, choose the roster and
harness, then press Ctrl-S to save. Press s to start the company.

The dashboard shows agent sessions, inboxes, captured terminal output, product
changes, and public tests. Select an agent and press Enter to read its terminal,
messages, notes, goals, and role. Other screens cover public tests, the product,
settings, the CEO's goal and result, activity, and the profession catalogue.
Press ? for keyboard help.

`vcomp run` and `vcomp start` open a running dashboard. `vcomp tui -root DIR`
views an existing company without starting supervision. Use `--plain`, or
redirect input/output, for the command-line interface.

## Guides

- [Terminal interface and keyboard controls](docs/tui.md)
- [Commands and operating notes](docs/usage.md)
- [CLI harnesses and local models](docs/harnesses.md)
- [Settings and templates](docs/configuration.md)
- [Company layout and communication](docs/company.md)
- [Engine behavior and design decisions](docs/engine.md)
- [Development instructions](AGENTS.md)

The engine supervises sessions; company behavior lives in editable prompts.
Agents continue running in tmux when their foreground supervisor stops. Use the
TUI's Stop action or `vcomp stop --plain` to stop the company completely.
