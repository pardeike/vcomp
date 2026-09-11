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
