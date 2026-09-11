# Running a company

[Project guide](../AGENTS.md)

## Interactive use

Bare `vcomp` opens the terminal interface. `vcomp run` and `vcomp start` start
supervision with the dashboard; `status`, `setup`, and `roles` open their related
screens. Unfilled `hire`, `steer`, `user-run`, `stop`, and `reset` commands open
forms or confirmations. Fully specified mutation commands execute immediately.

Use `--plain` for the original command-line behavior. Redirected input or output
also keeps the CLI. For example, `vcomp run -root DIR --plain` runs the foreground
supervisor and writes its log to standard output. `vcomp tui -root DIR` opens a
view without starting supervision.

See [the terminal interface guide](tui.md) for keys, screen behavior, and exit
semantics. Closing a view of another engine leaves it running. Leaving a run
started by this TUI stops its supervisor and keeps the agents in tmux; the
confirmation explains this. Use Stop to end both supervision and agent sessions.

## Operating notes

- Harnesses run with approvals bypassed (`claude --dangerously-skip-permissions`
  by default) — an agent stuck on a permission prompt looks idle and gets nudged
  forever. Run the whole thing under a scratch company root.
- Nudges are typed into the TUI as a single line then Enter, so prompts must stay
  one line; a newline would submit early.
- **Agents outlive their engine, on purpose.** Stopping `vcomp run` leaves the
  tmux sessions alive so you can pick the company back up with its memory
  intact - but they keep working unsupervised, with approvals bypassed, and
  they will do whatever their role implies. A tester handed a web product will
  start a server and drive a browser. `vcomp status` lists any vcomp sessions
  running on the machine, and `vcomp stop -all` ends them regardless of which
  company they belong to.
- **One engine per company.** `vcomp run` takes a pid lock in `.vcomp/`, because
  two engines on one company would each prod the same sessions and each read the
  other's restarts as its own. The kernel releases the lock on exit or crash. Stop/reset signal the owning
  engine and acquire its lock before touching sessions or company output.
- **Roles inherit your own global agent instructions**, by design. A `codex`
  role reads `~/.codex/AGENTS.md` and a `claude` role reads
  `~/.claude/CLAUDE.md` on top of its `role.md`, so your house conventions are
  company policy too. Useful, and worth remembering when an employee does
  something nobody in the company asked for.
- tmux session names are `<session_prefix>-<role>`. Give a second company a
  different prefix. Changing a prefix leaves existing sessions supervised under
  their original names; new sessions use the new prefix. `stop -all` finds owned
  sessions regardless of prefix, as well as registered engines without sessions.
- `vcomp attach <role>` opens the tmux session so you can watch someone work.

## Commands

```
vcomp                                   open the terminal interface
vcomp tui      [-root DIR]              view a company without starting its engine
vcomp start    [-root DIR]              start supervision with a dashboard
vcomp install  [-force]                 write the defaults to ~/.vcomp/
vcomp setup    [-root DIR]              ask for settings, save only what differs
vcomp run      [-root DIR] [-goal "…"]  keep the company alive (foreground)
vcomp roles    [-root DIR]              the role names a roster can contain
vcomp hire     NAME [-position P] [-backstory "…"] [-replace]
vcomp steer    NAME [-root DIR] [-text "…"] [-file FILE]
vcomp status   [-root DIR]
vcomp reset    [-root DIR] [-y]         start the run over, keeping the settings
vcomp user-run [-root DIR] [-instructions FILE] [-text "…"]
vcomp attach   [-root DIR] ROLE
vcomp stop     [-root DIR] [-all]
```

The whole thing is meant to start like this:

```
mkdir /tmp/vgame && cd /tmp/vgame && vcomp
```

Bare `vcomp` works on the current directory. If it is not a company yet, the
TUI opens the setup form. Ctrl-S validates and saves; Escape cancels without
writing anything. After setup, press s to start supervision. `vcomp start --plain`
retains the original setup questions followed by the foreground engine.

`vcomp reset` is the difference between starting over and starting from
nothing: it deletes everything the company produced - the spaces, the artifact,
the public runs, the engine's bookkeeping - and builds it again from the same
`vcomp.conf` and templates, so a run can be repeated without answering the setup
questions again. Flag-supplied goals are retained in `.vcomp/goal.txt` through a
`goal_file` setting, including multiline goals. Reset validates its inputs before
deleting anything and waits for the old engine to stop. It removes only known paths, never the directory it was given,
and asks before doing it.

`./install.sh` builds the binary into the first directory that is on your PATH
and writable - `~/Scripts`, `~/bin`, `~/.local/bin`, `/usr/local/bin`,
`/opt/homebrew/bin`, in that order, preferring user-owned ones so nothing needs
sudo - then runs `vcomp install` to populate `~/.vcomp/`. `BIN_DIR=… ./install.sh`
overrides the choice.

## Scripted profession management

These commands expose the same catalogue operations as the terminal interface.
The default scope is `company`; use `-scope global` for personal defaults.

```sh
vcomp profession list -root /tmp/vgame
vcomp profession show graphic-designer -root /tmp/vgame
vcomp profession generate example-role -brief "Describe the profession" > /tmp/example-role.md
vcomp profession create example-role -file /tmp/example-role.md -scope global
vcomp profession update example-role -file /tmp/example-role.md -scope global
vcomp profession delete example-role -scope global
vcomp profession restore example-role -scope global
```

Generation only prints a draft. Create and update validate the title, sector and
professional sections before writing. Listing includes deleted entries and the
source of each definition. Hiring remains `vcomp hire NAME -position PROFESSION`.

Send a one-off inbox message to any employee, including the CEO:

```sh
vcomp message ceo -root /tmp/vgame -subject "Graphic designer joined late" -text "Please coordinate with the new graphic designer."
```

Use `-file FILE` for a multiline message. On an interactive terminal,
`vcomp message ceo` opens the message form. The request is marked FROM USER and
URGENT in its folder name, but receives ordinary inbox handling.
