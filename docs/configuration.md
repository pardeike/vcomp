# Settings and templates

[Project guide](../AGENTS.md)

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

For the shipped CLI presets and local endpoint examples, see
[CLI harnesses and local models](harnesses.md).

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
Older rendered roles are converted using their profession and backstory, with
the original kept as `role.previous.md`. Unknown professions need an explicit
catalogue choice. Older flag-only goals are recovered before reset.

`standing.md` supplies the shared internal monologue in `notes.md`, concrete
outward communication, useful work when the inbox is empty, and personal goals
in `goals.md`. The engine has no opinion about that behavior.
