# Terminal interface

[Project guide](../AGENTS.md)

## Design

The terminal interface is the primary interactive entry point. Plain commands
remain available for scripts and agents with `--plain` or redirected input/output.
The interface reads company files and tmux; it does not assign work or judge the
product. Observed terminal output is labelled as output, not a model-generated
account of what an employee is thinking.

The main screens are:

- **Dashboard:** company and engine state, agents with inbox counts and recent
  terminal output, product progress, and public-run totals. Select an agent and
  press Enter for its terminal, inbox messages, notes, personal goals, and role.
- **Public tests:** run status, instructions, version, impressions, and abandonment
  details. Create a test from this screen.
- **Product:** git status and recent commits, with a diff view.
- **Settings:** common company settings and an editor for the full configuration.
  A new company starts with a form; cancelling writes nothing.
- **Goal / result:** the CEO's goal and final answer, preserving their actual text.
- **Activity:** the engine log, with scrolling and a return-to-latest action.
- **Role catalogue:** available professions, with a hire form.

Tab cycles screens; number keys select them directly. Arrows or j/k move through
lists, Enter drills down, Escape returns, and ? shows keyboard help. The header
carries the company name, engine status, and the age of the last observation;
the footer lists the keys that apply to the current screen, form, or chooser.
Errors stay visible with the entered values until the next key. Stop and reset
require confirmation; reset shows the company root and the paths that will be
removed. No action targets all companies implicitly.

Forms show one aligned row per field with a hint for the selected one. Fields
are typed, and only free prose is typed in: choices, rosters, paths, numbers,
and intervals use choosers.

- **Choice** (harness, model, effort, profession): Left/Right cycles, Enter
  opens a filtered list, and typing opens it already filtered. The typed text
  is also offered as a value of its own, so a model the harness knows but the
  list does not can still be entered. Model and effort suggestions come from
  the `models` and `efforts` keys of the selected harness section and swap
  when the harness changes.
- **Roster**: a profession list with counts; `0–9` sets the highlighted count,
  Return or Space toggles off/on with one employee, Tab accepts, and Escape
  cancels. Counts above one produce numbered names such as `developer-1` and
  `developer-2`; unchanged counts preserve existing names and order. The CEO
  count is limited to one.
- **Path** (goal file, CEO instructions, steering or test instruction files,
  company directory): a folder browser starting at the company. Enter opens a
  folder or picks a file, Backspace goes to the parent, typing filters. Files
  inside the company are stored relative to it. A path can still be typed.
- **Number** (idle ticks): Left/Right steps, digits type; empty inherits.
- **Interval** (tick, refresh): Left/Right walks a ladder from 500ms to 10m,
  Enter opens it as a list, and a duration can be typed.

The hire form suggests the next free name for the profession and follows the
profession until the name is edited. Fixed fields, such as which employee a
form is about, are shown but never selected.

At 120 columns and above the dashboard puts the selected agent's output beside
the roster. At ordinary 80-column widths it uses a compact roster and preview
below it. At narrow widths it keeps name, state, and inbox count, with detail on
Enter. Lists and documents scroll independently. Below 40 columns or 12 rows the
interface asks for more space while retaining exit keys. Resizing preserves the
screen, selection, and form contents. Color is supplementary; labels and selection
markers remain understandable without it.

The view refreshes independently of engine ticks. Quiet, broken, and startup
labels come from the latest engine observation; live/dead session checks come
from tmux. The last engine-observation time is shown so stale information is not
presented as current. An engine lock is checked, not inferred from a leftover PID
file. Closing a view of an existing engine leaves it alone. Leaving a run started
by this interface stops its supervisor and leaves the agents in tmux, matching
the existing foreground command behavior; the exit confirmation explains this.

The implementation uses the Go standard library, terminal escape sequences, and
`stty` on Unix. It restores terminal state on normal exit and handled signals.
Tests cover navigation, responsive layout, forms, choosers, read-only
observation, and terminal lifecycle. Real tmux fixtures exercise interaction without model calls.

## Keys and commands

| Key | Action |
| --- | --- |
| 1–7 / Tab | Dashboard, public tests, product, settings, goal/result, activity, roles |
| Arrows / j / k | Select or scroll |
| Enter / Esc | Inspect / go back |
| s / x / r | Start supervision / stop company / reset company |
| c / o | Company settings / open another directory |
| h / n | Hire an employee / create a public test |
| a | Watch the selected agent in tmux |
| t / b / p | Selected agent's steering / replacement / harness and pacing settings |
| e | Open the current editable document in `$EDITOR`, default `vi` |
| ? / q | Help / leave the interface |

When watching an agent, Ctrl-B then d detaches back to the TUI. When the TUI
itself runs inside tmux, watching switches sessions; Ctrl-B then L returns.
Generated role and goal documents are not edited through the viewer. Set a goal
file in Settings to edit a multiline goal, and use steering for employee
instructions. The full configuration remains available through the Settings
editor. Leave an employee setting empty to inherit the company default.

`tui_refresh` controls display refresh, default `2s`. It does not change `tick`.
The activity screen follows the latest log entries until you scroll; f resumes
following. Views limit large document reads to 256 KiB and label truncation.
Product diffs show tracked staged and unstaged changes; git status also lists
untracked files.

## Verification

Automated tests cover screen bounds at 160x48, 120x30, 80x24, 60x18, 40x12, and
smaller sizes; Unicode width and terminal-control filtering; navigation and
forms; invalid setup without writes; file-based goals; read-only observation;
and stale engine locks. Chooser tests cover filtered choice, typed values,
number and interval stepping, roster toggling, and the path browser. The CLI
integration test uses real tmux to hire through the profession chooser, steer,
create a public test, resize, visit an editor, cancel reset, leave an owned
supervisor, and verify terminal restoration while agents survive.

Manual Terminal checks cover the displayed dashboard and tmux attach/detach.
The fixtures use local shell agents and do not spend model API calls. A separate
pi check against the already downloaded Ollama `qwen3.5:9b` model verified read,
edit, and shell tools. That small check is not evidence for a whole company run.
