# Running a company

[Project guide](../AGENTS.md)

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
vcomp                                   set this directory up if needed, then run it
vcomp start    [-root DIR]              the same thing, named
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

Bare `vcomp` works on the current directory: if it is not a company yet it runs
the setup questions, and then it starts the engine. Leaving a new company without a goal
aborts and writes nothing at all, so running `vcomp` in the wrong
directory by accident costs you one keystroke.

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
