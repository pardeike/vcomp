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

## Non-goals

- No sandboxing between roles. Everything is public and cooperative by
  construction; the interesting failures here are social, not security ones.
- No scheduler, task graph, or orchestration DSL. If the company needs a process,
  the agents have to invent it and write it down.
- No structured message format. `message.md` is prose.

## Documentation

Read the relevant guide before changing that part of the project:

- [Terminal interface](docs/tui.md): primary user workflows, responsive screens,
  keyboard navigation, and the boundary between the view and supervision.
- [Company layout and protocol](docs/company.md): spaces, inboxes, public runs,
  CEO authority, replacement, steering, and completion.
- [Settings and templates](docs/configuration.md): configuration layers, command
  placeholders, professions, backstories, and generated role documents.
- [Engine and design decisions](docs/engine.md): session lifecycle, pacing,
  state, audit trail, logging, and tmux constraints.
- [CLI harnesses and local models](docs/harnesses.md): pi, Oh My Pi, OpenCode,
  provider setup, and session behavior.
- [Running a company](docs/usage.md): commands, setup, reset, installation,
  session ownership, and operating notes.

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
