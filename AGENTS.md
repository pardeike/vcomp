# vcomp — a virtual company simulation

`vcomp` runs a small company of AI agents. Each employee is a real CLI coding
agent (`claude`, `codex`, …) living in its own tmux session, its own folder, and
its own head. They coordinate **only through the filesystem** — no message bus,
no RPC, no shared memory.

Two rules shape the whole design:

- **The engine is dumb.** Its job is to keep everyone busy: start sessions, keep
  them alive, prod the motionless ones, and stop when it is told to. It has no
  opinion about the work, the people, or the product.
- **The behaviour lives in the prompts.** How a role thinks, speaks, decides,
  and fills an empty hour is written in that role's own `role.md`, not in Go.
  Whether this company works well is a property of those documents, and they are
  meant to be rewritten and tuned.

There are no constants in the code. Every timing, command line, model, effort,
prompt, and document is a setting or a template.

## Layout

```
company_root/
  CONVENTIONS.md          the shared protocol; every role is told to read it
  RESULT.md               written by the CEO; its existence ends the simulation
  spaces/                 one folder per employee
    ceo/
      role.json           profession, backstory, CEO steering (render inputs)
      role.md             generated identity, remit and behaviour
      goal.md             the goal (only the CEO is pointed at it)
      notes.md            their internal monologue, by convention
      goals.md            what they are personally trying to achieve
      inbox/<topic>/message.md
    developer-1/ art-director/ tester/ hr/ …
  product/                the artifact under construction — a git repo
  public/                 user-test runs
    run-0001/
      role.md             written by the engine: the throwaway user
      instructions.md     optional, from whoever asked for the test
      product/            snapshot of product/ at run start, without .git
      version.txt         the commit the snapshot came from
      impressions.md      written by the user; its existence ends the run
  .vcomp/
    vcomp.conf            this company's settings — usually a few lines
    templates/            this company's template overrides (optional)
    state.json engine.log engine bookkeeping
```

Nothing is secret. Every role may read every space, every public run, and the
product repo. The goal is "known only to the CEO" by *convention*: it lives in
`spaces/ceo/goal.md`, and only the CEO's `role.md` mentions it. Nobody is
prevented from looking — they are just never told to.

## Settings

Three layers, each overriding the one before:

1. **built-in defaults** — `internal/config/default.conf`, compiled in
2. **`~/.vcomp/vcomp.conf`** — your defaults for every company
3. **`<company>/.vcomp/vcomp.conf`** — this company's overrides

So an empty directory is already a valid company root, and a company's own
config is short enough to read at a glance. `vcomp install` writes layer 2;
`vcomp setup` asks for each setting and writes only the answers that differ.

The format is `key = value` lines with `[section name]` headers. A value is the
rest of the line, so it may contain spaces, `=` and `#`; only whole-line
comments. Unknown keys are an error, so typos surface immediately. All files are
re-read every tick — edits take effect live, and a broken file leaves the last
good configuration running.

Harness command lines are templates: `{{model}}` and `{{effort}}` are
substituted, and an empty value removes its token *and the flag it belonged to*,
so `--model={{model}}` with no model set disappears rather than passing an empty
argument. Anything settable globally can be set for one person:

```
[role ceo]
harness = codex
model = gpt-5.1-codex-max
effort = high
idle_ticks_empty = 20
nudge = Stop writing plans. Go ask someone a hard question and demand evidence.
```

## Templates

Every document an agent reads is rendered from a template, resolved the same
way: `<company>/.vcomp/templates/`, then `~/.vcomp/templates/`, then the copies
built into the binary. `standing.md` supplies the behaviour every role shares,
and `backstories/*.txt` supply personal histories, picked deterministically from
the role's name - so `developer-1` and `developer-2` share a template and get
different people.

Every profession is a `positions/<name>.md` template with a title, a backstory
sector, a remit and a bias. All 29 professions, including the CEO, render through
`role_position.md` with `standing.md`. `vcomp roles` lists one catalogue. There
is no generic fallback and no freeform role document path. Adding a profession
means the user adds a template using the same override directories.

The default roster is `ceo, project-manager, developer, designer, art-director,
tester`. Names ending in `-N` share a profession, so a company may still hire
`developer-1` and `developer-2`. An arbitrary personal name needs `-position`.

**A role is a fixed profession plus a free backstory.** Experience and temperament
vary; the profession and standing rules do not. The default background and trait
are picked deterministically from the role name. HR may use
`vcomp hire NAME -root ../.. -position POSITION -backstory "..."` to provide a
background. It must not turn that background into new duties or instructions.

Only the CEO may add a final steering section, using
`vcomp steer NAME -root ../.. -text "..."` or `-file FILE`. Empty text clears it.
Steering narrows the work within the profession; it never replaces its template.
Replacing an employee with `hire -replace` changes the backstory and retains the
profession and any CEO steering. Removing the space removes the employee.

The CEO has its own fixed profession template covering its basic responsibilities
and the engine protocol. Its backstory comes from the configured template pool.
The user alone supplies additional instructions using `ceo_instructions_file`, a
text file relative to the company root or an absolute path. These are appended
using `ceo_tweaks.md`. Neither HR nor the CEO can hire, replace or steer the CEO
through the employee commands. The fixed CEO template is user-configurable in
`positions/ceo.md`, with the same company/global/built-in resolution as all other
texts. The user also owns all other profession and shared templates.

Each employee's `role.json` holds the composition inputs. The engine regenerates
`role.md` from those inputs and the templates each tick, publishing only changed
content. A manual edit to generated role.md is repaired; it cannot replace the
profession. Missing generated documents are recreated from their inputs. Agent
authority remains a prompt convention, not filesystem permissions or a sandbox.

`standing.md` supplies the shared internal monologue in `notes.md`, concrete
outward communication, useful work when the inbox is empty, and personal goals
in `goals.md`. The engine has no opinion about that behavior.

## The protocol

**Inbox.** `spaces/<role>/inbox/<topic>/message.md`, plus attachments. To message
someone you `mkdir` a topic folder in *their* inbox. That is the whole API.

There is no delivery receipt and no ack. You learn a message landed when the
recipient deletes the topic folder, sends something back, or the change appears
in `product/`. Every role must prune its own inbox: handled or rejected, the
folder goes. An inbox that grows forever is a visibly failing role.

Messages are requests, not orders — they may be negotiated or refused.

**Product.** `product/` is a git repo and the only thing that ultimately matters.
It is the shared, observable state: reading the diff is how roles find out what
everyone else has been doing.

**Public runs.** `public/run-NNNN/` is a user test. Anyone may create one. The
engine snapshots `product/` into it and starts a throwaway agent that has never
seen the company and never will again, which leaves `impressions.md` behind.
Everyone can read every impression. It is the only unfiltered outside signal the
company gets.

**The CEO.** One overseer, knows the goal, reads everything. **Never judges the
product and never does work** — it may only delegate, ask critical questions, and
demand evidence from the people whose job it is to judge.

**Replacement and steering.** Changing an employee's composition inputs changes
its generated role.md. The engine notices, closes the old session and starts a
fresh conversation. Notes and the space survive. The CEO uses `hire -replace`
for a new backstory or `steer` for an added instruction; neither replaces the
fixed profession. The CEO's own instructions come only from user settings.

**Ending.** The company stops when `RESULT.md` (configurable) appears in the
root. Only the CEO writes it, and it holds the final answer handed back to
whoever set the goal. The engine then closes every session and prints the file.
Declaring the goal *met* is not the same as judging the product good: the CEO is
required to base it on what the tester, the art director, and the public runs
actually showed.

## The engine

A tick loop (default 20s). Per tick:

1. Reload settings. Stop if the result file exists.
2. **Discover** — each employee has `spaces/*/role.json` composition inputs and a
   generated `role.md`. Missing generated documents are restored.
3. **Hire** — a space with no session gets one:
   `tmux new-session -d -s vcomp-<role> -c <space> <harness>`, then the
   harness's `handshake` keys on the next tick if it has any, then a one-line
   prompt on the tick after, once the TUI has drawn itself. A role we have run
   before is restarted with the harness's resume flag so it keeps its memory.
4. **Replace / steer** — if the generated `role.md`'s hash changed, kill and restart *without*
   resume. New person, empty head. This is the **only** thing that ends a living
   session: a new model, a new effort, even a different harness never kill one,
   because the history inside a running session is the entire point of keeping
   it running. Those changes apply the next time the role has to start anyway.
5. **Revive** — session gone or its pane exited → recreate it. Panes are kept
   after their command exits (`remain-on-exit`), so a harness that dies on
   startup leaves its error behind to be read rather than being restarted
   forever in silence. A resume that fails before its first prompt is retried fresh; an established
   conversation continues to use resume after later exits. After `max_restarts` failures the
   role is marked broken, the command and the pane's last lines are logged
   once, and it is left alone until `role.md` or the settings change.
6. **Nudge** — a session whose pane text is byte-identical for N consecutive
   ticks is stuck, so type the nudge prompt at it. Because agents animate while
   thinking, a busy one never looks idle; this needs no harness-specific parsing.
7. **User runs** — a `public/run-*` without `impressions.md` and without a live
   session gets a fresh ephemeral agent; when the file appears the session is
   killed. Runs that fail twice get an `abandoned.txt` and are skipped.

The CEO is treated exactly like everyone else, so it runs in parallel and is kept
alive by the same rules.

**Pacing.** N above is not one number. A role with something in its inbox is
prodded after `idle_ticks` (3); a role whose inbox is empty is left alone for
`idle_ticks_empty` (9), so people with nothing waiting tick over slowly instead
of filling the time with invented personal projects. Both are settable per role,
which is the hook for a future version where the CEO — or the project master,
delegated — throttles a specific idle role by writing to the config the engine
already re-reads every tick.

**The state file.** Every tick the engine rewrites `STATE.md` in the company
root: per role its session, inbox depth, idle ticks and when its space last
changed, plus the product's commit count and last commit, and the public run
tally. Nobody is sent it. It is deliberately pull, not push - an overview pushed
into a prompt is just something else filling a context window, and the whole
communication design here is that you go and look. It exists mainly so the CEO
can spot a bottleneck (an inbox that keeps growing) or a passenger (a space that
has not changed while the product has) without reading six directories.

**The audit trail.** Every message that passes through an inbox is appended to
`.vcomp/messages.jsonl` as one JSON object per event: `sent`, `updated`,
`cleared`, with the prose, the recipient, the `From:` line if there is one, how
deep that inbox was at the time, and how long the message sat there before it
went. Attachments are counted, not copied - they are already in the repo.

The engine **remembers rather than intercepts**. Agents create and delete inbox
folders directly, which is the entire communication design, and putting the
engine in the middle of it would change the thing being measured. So each tick
it reads the inboxes, records what it has not seen, and notices what has gone; a
message seen once is preserved even if its folder is deleted a second later. The
honest gap is one tick wide: a message created and deleted inside a single tick
is never observed, and a message that short-lived was not read by its recipient
either.

**The log reports changes.** Hires, recovered or replaced sessions, startup
handshakes and prompts, failures, public run outcomes, inbox depth changes and
new product commits are visible. A quiet role's first nudge is reported; identical
quiet/nudge observations are suppressed. Relative timestamps and idle counters
do not produce repetitive log lines. Invalid configuration is reported once per
changed error, then recovery is reported when it becomes valid again.

That is the entire engine. Everything else is emergent.

## Non-goals

- No sandboxing between roles. Everything is public and cooperative by
  construction; the interesting failures here are social, not security ones.
- No scheduler, task graph, or orchestration DSL. If the company needs a process,
  the agents have to invent it and write it down.
- No structured message format. `message.md` is prose.

## Decisions worth knowing

Things that look arbitrary and are not, so nobody "fixes" them back:

- **The prompt points at `role.md`; it is not injected into the context.** A
  nudge is typed into a TUI, so it has to be one line. The agent re-reads
  the generated document, including its backstory and any steering.
- **The original agent pane is the liveness check.** tmux retains it on exit.
  Supervision uses its pane ID even if someone attaches and selects another
  window or split. If that original pane disappears, the engine recreates the
  agent. Session ownership and startup state are also saved in tmux options,
  allowing recovery when state.json is missing or damaged. A session owned by
  another company is never adopted because its prefix happens to match.
- **The user role's blindness is instruction, not enforcement.** Everything is
  world-readable by design; a user agent that goes looking for the company can
  find it. The snapshot in `public/run-*/product/` exists so it has no reason to.
- **Both harnesses ask "do you trust this folder?" the first time they run in a
  directory**, with "yes" preselected. Typing a prompt into that dialog answers
  it wrongly and quits the agent, which used to produce an endless hire-and-die
  loop that built nothing. That is what `handshake = Enter` is for. It is a
  per-harness setting rather than engine code because the next harness will ask
  something else.
- **A dialog is invisible to idle detection.** A harness sitting on a question
  looks exactly like a harness thinking, so the engine cannot discover this by
  watching; it has to be told what to press.
- **`tmux -t =name` only works for session targets.** Pane targets
  (`capture-pane`, `send-keys`) take the bare name; the `=` form fails with
  "can't find pane".
- **An empty placeholder removes its flag, not just its token.** Dropping only
  `{{model}}` would leave a dangling `--model` and the harness would refuse to
  start.

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

## Working on this repo

Go, standard library only - no dependencies, and it should stay that way. Keep
it native and small. Do not overengineer or over-harden: this is a simulation of
a cooperative company, and the interesting failures are social, not adversarial.
When something needs a knob, it goes in `default.conf` or a template, not into
the code.

Tests are for checking assumptions, not for ceremony. Run them when you have
changed something you are unsure about, not after every edit - the engine suite
drives real tmux and takes about 17 seconds, so wasteful runs are genuinely
wasteful.

Finish each piece of work by committing it and then running `./install.sh`. The
installed binary is what gets used from a company directory, so a commit that is
not deployed means the next test run exercises the previous version - which is
its own species of confusing bug.

## Testing

Tests run the real tmux and a fake harness — a shell script that records how it
was started and then echoes whatever is typed at it — so the whole loop (hire,
prompt, resume, replace, nudge, pace, user run, finish) is verified without
spending a single API call. `VCOMP_HOME` redirects the global settings directory
so tests never see your own `~/.vcomp`.
