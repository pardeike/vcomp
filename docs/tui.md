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
  press Enter for its inbox messages, notes, terminal output, personal goals, and role.
- **Public tests:** run status, instructions, version, impressions, and abandonment
  details. Create a test from this screen.
- **Product:** git status and recent commits, with a diff view.
- **Settings:** common company settings and an editor for the full configuration.
  A new company starts with a form; cancelling writes nothing.
- **Goal / result:** the CEO's goal and final answer, preserving their actual text.
- **Activity:** the engine log, with scrolling and a return-to-latest action.
- **Role catalogue:** shared profession definitions, their source and status,
  with create, inspect, edit, delete, restore, generate, and separate hiring actions.

Main screen names stay the same at every terminal width. When all screen tabs
do not fit, the header shows the current screen name and number. Tab cycles
main screens; number keys select them directly. In employee detail, Tab and
Shift-Tab instead change the Inbox, Notes, Terminal, Goals, and Role tabs. Arrows or j/k move through
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
| a | Enter interactive intervention, after showing return instructions |
| t / b / p | Selected agent's steering / replacement / harness and pacing settings |
| e | Open the current editable document in `$EDITOR`, default `vi` |
| ? / q | Help / leave the interface |

When intervening in an agent, Ctrl-B then d detaches back to the TUI. When the TUI
itself runs inside tmux, intervention switches sessions; Ctrl-B then L returns.
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

## Dashboard progress

As width becomes available, the role table progressively adds `#`, `Started`, and `Avg`
columns, prioritizing them over CLI and idle counts. `#` counts completed prompt-to-final-response assignments in the
current OMP conversation. Thinking, tool calls, automatic model retries, and
server queue time belong to that assignment. `Started` is the active turn's
local start time, with no repeated label in individual rows. The average uses
completed turns only; unfinished, aborted, and unrecovered error turns are
excluded. A model response stopped at its output limit counts as finished, not
necessarily successful.

Timing comes from OMP's recorded session history, not animated terminal output
or the engine's idle counter. It becomes visible when OMP persists its records.
Missing or unsupported history shows `—`, not a fabricated zero. Resuming the
same conversation retains its counts; a fresh conversation starts new counts.
The measurements are display-only and never influence supervision or prompts.

The selected employee's preview lists up to three inbox directory names, with
a count of additional requests. The Inbox tab in employee detail shows one request at a time, with its
directory name and position (for example, `Request 2 of 5`). Left/Right or
`[` / `]` selects the previous/next request; Up/Down, Page Up/Down, and Home/End
scroll its content. Requests are ordered by directory name. New arrivals keep
the current selection; if the selected request disappears, the view selects
the next available request, or the last one if it was at the end. Taller dashboards show up to three recent product
commit messages above the role table; the Product screen retains the longer
history. These additions shrink or disappear when the terminal lacks space.

## Profession catalogue

On screen 7, Enter reads the selected profession's fixed definition, including
its remit, bias, backstory sector and source file. `h` hires a person from that
profession; the hire form's backstory belongs only to that person.

`n` creates a profession, either generated or written manually. `e` edits the
selected definition in `$EDITOR`; `g` generates a replacement draft. Both paths
show a draft first. Use `e` to revise it, then `s` or Ctrl-S to validate and save
with confirmation. Escape leaves the draft without installing it. Choose
**My defaults** for all companies or **This company only** for an override.
Changing an existing profession changes its employees' rendered roles and causes
the engine to replace their conversations on the next tick.

`d` deletes a profession from hiring, retaining its definition for existing
employees. `z` shows deleted professions and `u` restores one. A company
can override a global deletion with its own saved definition.

`p` opens role generation settings; the same form is available with `g` on
Settings. Model, effort, timeout and CLI command can be saved globally or for
one company. These settings are independent of employee and public tester
models. The display continues refreshing during generation; Ctrl-C cancels and exits.
Generation does not hire anyone.

## Messages from the user

Select an employee on the dashboard, or open any of their detail tabs, and press
`m`. Enter a subject and message, or choose a file for a multiline body, then
Ctrl-S sends it. This works for the CEO too.

The request folder is `URGENT - FROM USER - <subject> - <unique suffix>`.
The subject is sanitized for filesystem use, without colons or path separators.
The message itself starts with `FROM USER`. Delivery does not interrupt the
employee, alter their role, or force reading. It follows normal inbox handling.
Steering remains a persistent instruction change and is separate from messages.

Employee detail identifies the current role in uppercase reverse video above
the content. The top row starts with the company name, without a vcomp prefix.

## Table sorting and public test progress

On Dashboard, Public tests or Role catalogue, press `S` to choose a sort field
and direction, then Ctrl-S saves it for this company. Each table remembers its
own setting after restart. Refresh and sorting preserve the selected item by
name. Dashboard supports name, state, inbox count, completed turns, current
turn start, average turn duration and harness. Catalogue supports name, title,
sector and active/deleted state. Public tests supports name, creation time,
state and attempt count. Ties use names for stable ordering.

The public tests table shows creation date and time plus pending, waiting,
starting, evaluating, retry pending, done or abandoned state. Evaluating means
the reviewer session is alive; it does not prove ongoing model progress. Done
means impressions exist, not that the product passed. Wider terminals also
show the attempt count and a literal excerpt from impressions or abandonment.
Enter opens the full text and timestamp. macOS uses filesystem birth time;
on other platforms `~` marks an estimate from directory modification time.

## Passive terminal activity

The Terminal tab defaults to a read-only activity feed from OMP's session
records. It uses the viewer's width rather than copying OMP's narrower layout.
Records appear in normal chronological order, with timestamps, tool calls,
results and responses. The view follows the newest output at the bottom.
The current turn's elapsed time and latest-record age are shown explicitly.
When available, the live terminal's current-operation line is shown verbatim.
An active generation may not be persisted yet; a quiet transcript does not prove
that the agent has stopped or is thinking rather than waiting for inference.

`v` cycles brief, detailed and raw views and saves `terminal_view` in this
company's settings. Brief shows tool intents and the first line of messages and
results. Detailed includes recorded arguments and full text, within the normal
256 KiB display limit. Raw shows the captured terminal with its original layout.
Harnesses without supported session records fall back to the raw capture.

`a` is **intervene**, an interactive attachment for technical intervention.
Before attaching, the interface explains that keys reach the agent and that
Escape may interrupt it. Ctrl-B then d returns when attached from a regular
terminal; Ctrl-B then L returns when vcomp itself is inside tmux. For ordinary
observation, remain in the Terminal tab, where keystrokes never reach OMP.

## Document boundaries

Settings hides blank lines in the configuration preview without rewriting the
file. A labelled divider separates the file contents from vcomp's controls and
settings explanation. Product status/commits, staged/unstaged diffs, Goal/Result,
public test files, and profession definitions use the same labelled dividers.
Keyboard hints remain in the footer rather than following product content.

Employee detail separates the role tabs from the document with a rule. Terminal
status is followed by a labelled Recorded activity or Raw terminal divider.

The Terminal tab automatically follows new activity. Scrolling, Home or End
pauses a snapshot of the document, so incoming records cannot shift the text
being read. `f` resumes live updates and scrolls to the newest output. The footer
shows following or paused. Entering Terminal or changing verbosity resumes
following. This also applies to the raw terminal view.

## Direct steering and broadcasts

Press `i` on the dashboard or inside a role to send a **Direct steer** into that
employee's existing conversation, including the CEO's. Press `B` on the dashboard
for **Broadcast** to all currently running employees. Public testers are excluded.
Both forms accept prompt text or a file, and offer two delivery modes:

- **Queued**, the default: keep the prompt in vcomp until the employee reaches
  an available input prompt. It takes precedence over the next automatic nudge.
- **Immediate**: interrupt the active turn, wait for an available input prompt,
  then submit the instruction and let the same conversation continue. Claude's
  restored cancelled prompt is cleared before submitting the replacement.

The Terminal tab shows the number of pending direct prompts. Submission results
appear after sending and in Activity, per employee for broadcasts. A submission
means the terminal accepted the send operation, not proof that the model obeyed.
Unknown UI states fail without typing blindly. If interruption succeeds but input
never becomes available, automatic prompts for that employee stay paused; inspect
its terminal and retry Direct steer once the input is available. Existing native
harness queues or unsent editor text may need interactive intervention.

This is separate from `m` inbox messages, `t` persistent role steering, and `a`
interactive terminal intervention. Direct steering preserves the conversation
and does not edit the role, backstory or inbox. Immediate delivery goes ahead of
vcomp's queued prompts; earlier queued prompts remain pending.

The dashboard labels inbox depth `In` and sizes columns to their current contents.
`Activity` shows the latest recorded event, falling back to terminal text when
records are unavailable; border-only lines are omitted. It is the latest observed
activity, not proof that the same operation is still running.
