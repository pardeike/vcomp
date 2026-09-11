# CLI harnesses and local models

[Project guide](../AGENTS.md)

vcomp starts each employee's CLI in tmux, with the employee's space as its
working directory. The CLI owns model access, tools, conversation history, and
context compaction. vcomp supplies prompts and supervises the session.

## Choose a harness

The built-in presets are `claude`, `codex`, `pi`, `omp` (Oh My Pi), and
`opencode`. Install the CLI separately and make sure it is on `PATH`.

Set a company-wide default before any section headers in `.vcomp/vcomp.conf`:

```ini
harness = pi

[harness pi]
model = local/my-model
```

Or select a different harness and model for one employee:

```ini
[role developer]
harness = omp
model = local/my-model
```

Use the full `provider/model` selector. The provider name belongs to the chosen
CLI's configuration; it is not a new vcomp setting. Configure the endpoint there
first. The examples below use an illustrative model ID and server address;
replace both with your server's values and merge into any existing configuration.

## pi

Add a provider in `~/.pi/agent/models.json`:

```json
{
  "providers": {
    "local": {
      "baseUrl": "http://127.0.0.1:8080/v1",
      "api": "openai-completions",
      "apiKey": "local",
      "models": [{ "id": "my-model" }]
    }
  }
}
```

The dummy key is for a server without authentication. For an authenticated
server, use pi's credential configuration. Select it with `model = local/my-model`.
The preset maps `effort` to `--thinking`; leave it empty unless the model
supports a thinking control. Pi has no built-in tool approval dialog.

See [pi custom models](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/models.md)
and [CLI documentation](https://github.com/earendil-works/pi/tree/main/packages/coding-agent).

## Oh My Pi (omp)

Add a provider in `~/.omp/agent/models.yml`:

```yaml
providers:
  local:
    baseUrl: http://127.0.0.1:8080/v1
    api: openai-completions
    auth: none
    models:
      - id: my-model
```

For an authenticated server, configure `apiKey` and remove `auth: none`.
Select it with `model = local/my-model`. The preset maps `effort` to `--thinking`
and passes `--auto-approve` so tool approval prompts do not stall an employee.
It also sets `OMP_SKIP_SETUP=1` to skip the first-run onboarding wizard. Configure
the provider before starting vcomp; the preset does not perform that setup.
`omp models find local` checks that the provider's models are available.

See [OMP model configuration](https://github.com/can1357/oh-my-pi/blob/main/docs/models.md).

## OpenCode

Add a provider in `~/.config/opencode/opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "local": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Local server",
      "options": {
        "baseURL": "http://127.0.0.1:8080/v1"
      },
      "models": {
        "my-model": { "name": "My local model" }
      }
    }
  }
}
```

Select it with `model = local/my-model`. The preset passes `--auto` to approve
tool requests that are not explicitly denied. Existing deny rules still apply.
Configure thinking options and variants in OpenCode; this preset does not map
vcomp's `effort` to a CLI flag.

See [OpenCode providers](https://opencode.ai/docs/providers/),
[configuration](https://opencode.ai/docs/config/), and [CLI flags](https://opencode.ai/docs/cli/).

## Separate public testers

Set `[user]` `harness`, `model`, and `effort` to use a different CLI/model for
public user tests. This can pair local-model employees with a frontier-model
reviewer without changing the model inherited by new hires. The selected CLI
must already be installed and authenticated. See [public tester settings](configuration.md#public-tester-model)
for inheritance rules and an example.

## Sessions and startup

All three presets use `--continue` for revival and omit it for a new or replaced
employee. Pi and OMP store sessions by working directory. Keep OpenCode's
session directory filtering enabled so continuation stays in the employee's
space. Avoid starting unrelated conversations in employee spaces: continuation
selects the latest conversation, not an explicit session ID.

These presets have no startup handshake. If a CLI version or extension adds an
interactive startup question, resolve its setup before starting the company or
configure that harness's `handshake` keys. Command lines can be overridden using
the existing `[harness NAME]` settings. Installed CLIs must support the flags in
the preset; check `--help` when upgrading or diagnosing startup failures.

Changing a harness or model does not interrupt a living employee. The new
settings apply when it next starts. Switching harnesses starts a fresh
conversation; histories cannot be resumed across CLIs.

## Local server checks

A chat response alone does not prove that an employee can work. Check a small
file edit and shell command with the selected model before running a company.
The server and model must support tool calls. Set context and output limits to
match the actual model, and use the CLI's compatibility settings when the
endpoint rejects reasoning parameters or message roles.

Several employees share the server's inference capacity. Start with a small
roster and use the existing `idle_ticks` and `idle_ticks_empty` settings to pace
nudges. Model serving and request scheduling remain the server's responsibility.

New presets come from the installed binary even if `~/.vcomp/vcomp.conf` was
written by an older version. Re-running `./install.sh` preserves existing
settings; there is no need to overwrite them with `-force`.

## Verified versions

On 2026-09-10, pi 0.75.5, OMP 18.1.16, and OpenCode 1.18.25 were checked in
real tmux sessions against a controlled OpenAI Chat Completions endpoint.
The checks covered prompt submission, a shell command writing a file, recovery
of the correct employee's history with two employee directories, and a fresh
conversation after replacement. OMP's check used the preset's setup-skip option.

The endpoint returned scripted responses. This verifies CLI integration, not
the tool-calling reliability or performance of an actual local model.

## OMP turn timing

The OMP preset sets `turn_history = ~/.omp/agent/terminal-sessions`. The observer
uses the original tmux pane's terminal name to find OMP's active-session
breadcrumb, validates its working directory against the employee space, and
reads that JSONL conversation. It caches unchanged transcripts between display
refreshes. It does not send commands or install extensions in OMP.

Override `turn_history` in `[harness omp]` if OMP uses another agent directory,
or leave it empty to disable this measurement. The directory must contain OMP
terminal breadcrumbs and reference OMP-format JSONL sessions. Other shipped
harnesses currently leave it unset and display unavailable timing. See
[dashboard progress](tui.md#dashboard-progress) for the counting rules.

## Prompt readiness and queued messages

Employees and public testers only receive automatic prompts when their running
harness matches its configured `ready_pattern` and does not match
`busy_pattern`. These are Go regular expressions over the original pane's
visible text, with nonbreaking spaces normalized. Idle pacing begins after this
check. Unchanged text during inference or tools no longer triggers nudges.

The supplied patterns cover the installed OMP 18.1.17, pi 0.75.5, Claude Code
2.1.268, Codex 0.154.0, and OpenCode 1.18.30 interfaces. Idle, busy and completed
frames were checked for each. pi, OMP and OpenCode used a deliberately delayed
local response fixture; Claude and Codex used short shell-sleep tasks.

OMP's idle brand is `π`; during a turn it becomes a spinner and elapsed time.
Codex reports Ready or Working in its footer. Claude and OpenCode expose
interrupt controls while working; pi exposes its working loader. Pending
messages and retry/compaction indicators also prevent submission.

These checks are conservative UI observations, not a harness-side atomic API.
Changed themes, versions or layouts can fail to match, in which case vcomp waits.
A custom harness needs an explicit `ready_pattern`; an absent pattern disables
its automatic prompts. Do not use a match-all pattern for a real agent. Startup
trust/login dialogs may require interaction before readiness can be established.

Readiness is checked again immediately before sending. A submitted ready-frame
fingerprint is persisted to prevent duplicate submissions before the UI changes.
Existing queued messages are not removed or rewritten by this change.

## Direct steering controls

Native queue semantics differ: [pi](https://github.com/earendil-works/pi/tree/main/packages/coding-agent#message-queue)
separates steering from follow-up messages, and
[OMP](https://github.com/can1357/oh-my-pi/blob/main/docs/rpc.md) exposes steering,
follow-up and abort-and-prompt operations. vcomp holds queued direct prompts
outside the harness and submits only at its recognized available input prompt,
so queued delivery consistently waits for the current work to finish.

Immediate direct steering uses each preset's `interrupt = Escape` and
`interrupt_pattern` to recognize an active turn that can be interrupted. An
available input prompt needs no interrupt. Escape is documented by
[Claude Code](https://code.claude.com/docs/en/interactive-mode), pi, OMP and
[OpenCode](https://opencode.ai/docs/keybinds/), and is displayed by the installed
Codex interface. Custom bindings, Vim modes and extensions may change behaviour;
override the preset keys and patterns to match. Unknown states are rejected.

`direct_steer_timeout = 15s` bounds waiting for the input prompt after interruption;
`direct_steer_poll = 200ms` controls checks during that wait. The supervisor
serializes direct delivery and automatic prompts. A broadcast interrupts each
eligible employee before waiting for any one to finish cancelling. Conversation
history survives; an interrupted external tool can still have side effects.

Claude can restore the cancelled prompt into its editor before generating its
first response. Its preset also defines `interrupt_clear = C-u` and
`interrupt_input_pattern` to clear that restored draft once the busy indicator
has gone. This applies only after vcomp itself interrupted the turn. It does not
clear a pre-existing draft in an otherwise idle session. Other native queued
messages are not discarded. Live post-interrupt and restored-input frames are
covered by the readiness fixtures.

## Optional company tools

`mcp_enabled = true` is the default. When vcomp starts an employee, it supplies
six optional tools through a built-in MCP server. No separate server install,
API key, network listener, or port is needed. Each harness launches its own
`vcomp mcp -root <company> -role <employee>` subprocess over stdin/stdout.
Both arguments are explicit, and the root is resolved to its canonical path.
Two companies can use identical employee names without sharing tool state.
They still need distinct `session_prefix` values for their tmux sessions, as
before; MCP adds no shared port or global configuration to coordinate.

| Tool | Information or action |
| --- | --- |
| `company_overview` | Company paths, employee names and professions, observed state, inbox counts, turn statistics when available, three recent commits and five recent public-run states |
| `role_read` | Existing role, conventions, notes or personal goals, in pages |
| `inbox_list` | Exact topic names, short previews and modification times |
| `inbox_read` | A topic's message and attachment names; reading leaves it in place |
| `message_send` | Publish an ordinary inbox message with the sending employee's identity |
| `inbox_remove` | Remove a specific handled topic and its attachments from the caller's inbox |

Harnesses may prefix tool names. OMP versions using device tools expose these
as `xd://mcp__vcomp_company_<tool>` through their ordinary read/write tools. Lists and text are paginated, defaulting to 20
entries or lines and capped at 100. Long lines and previews are explicitly
shortened; the full files remain available. An overview reports observations,
not an assessment of productivity. It includes observation timestamps where
available. Sending a message does not interrupt anyone or establish that it was
read, and employee messages do not get the user's `URGENT - FROM USER` prefix.

The tools wrap the existing filesystem conventions. They do not add priorities,
assign work, change product editing, or replace any role's instructions. Ordinary
file tools remain equally valid. The overview does not include the CEO's goal,
source files, or private public-tester instructions. Public testers receive no
company-tool integration.

The five supported harnesses are wired without changing user-global settings:

- **OMP:** a managed `vcomp-company` entry in the employee's `.omp/mcp.json`;
  other server entries and settings are preserved.
- **Claude:** `--mcp-config` points at an employee-specific generated JSON file.
- **Codex:** command-line `mcp_servers.vcomp-company` configuration overrides.
- **OpenCode:** a process-local `OPENCODE_CONFIG_CONTENT` entry, preserving
  unrelated inline settings and merging with its ordinary configuration.
- **pi:** a bundled extension supplied with `--extension`, translating these
  six MCP tools into pi tools. It needs no additional package installation.
  `mcp_timeout = 30s` controls its request deadline. Other CLIs use their own
  MCP timeout settings.

Generated Claude and pi files live under `.vcomp/mcp/<employee>/`. The reserved
server name is `vcomp-company`. Custom harness presets are left unchanged;
configure their MCP client manually with the stdio command above if desired.

Set `mcp_enabled = false` before section headers to omit the integration on the
next employee start. Configuration changes do not restart existing conversations.
An already-running OMP session can load a prepared project configuration with
`/mcp reload`; merely restarting the vcomp supervisor does not prepare or reload
MCP for an existing employee. A failed preparation is logged and the employee
still launches with its ordinary tools. Harness-level MCP permissions and
server deny lists retain their normal effect.
