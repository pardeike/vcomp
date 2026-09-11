# Engine and design decisions

[Project guide](../AGENTS.md)

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
   conversation continues to use resume after later exits. Failures to create or launch a tmux pane are retried after
   `launch_retry_delay` (default 1m), without consuming the harness crash allowance.
   The error remains visible while waiting and clears when launch succeeds.
   After `max_restarts` harness exits the
   role is marked broken, the command and the pane's last lines are logged
   once, and it is left alone until `role.md` or the settings change.
6. **Nudge** — require a recognized, available harness input prompt, then use
   byte-identical frames for the configured idle pacing. Frozen terminal output
   alone never proves readiness. Busy, queued, retry, and unknown states wait.
   Initial and resumed prompts use the same readiness check. A sent ready frame
   is remembered across supervisor restarts, so an unacknowledged submission is
   not repeatedly sent into an unchanged UI.
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
- **Some harnesses ask "do you trust this folder?" on first use**, with "yes"
  preselected in the original Codex setup. Typing a prompt into that dialog answers
  it wrongly and quits the agent, which used to produce an endless hire-and-die
  loop that built nothing. That is what `handshake = Enter` is for. It is a
  per-harness setting rather than engine code because the next harness will ask
  something else.
- **Unknown UI states wait.** Initial trust/login dialogs must be resolved
  before a regular prompt is sent. A configured startup handshake remains a
  separate action. It is not evidence that the harness is ready for a prompt.
- **`tmux -t =name` only works for session targets.** Pane targets
  (`capture-pane`, `send-keys`) take the bare name; the `=` form fails with
  "can't find pane".
- **An empty placeholder removes its flag, not just its token.** Dropping only
  `{{model}}` would leave a dangling `--model` and the harness would refuse to
  start.

## User control channel

The supervising process owns a local Unix socket, accessible only to its OS
user, for direct steering requests. The CLI and TUI submit requests there;
only the supervisor event loop touches harness input. Direct interruption and
submission therefore cannot interleave with automatic nudges. The socket name
uses the canonical company path's hash so long company paths and symlink aliases
work. It is created only while holding the existing supervisor lock.

Queued direct prompts are kept in employee state and tmux recovery metadata,
and are consumed before normal idle nudges. Immediate requests bypass this queue
without discarding it. A failed immediate handoff holds automatic prompts until
another direct request releases the hold. This control channel is for explicit
user intervention; employee-to-employee communication remains filesystem based.
