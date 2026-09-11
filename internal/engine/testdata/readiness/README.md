# Readiness fixtures

Captured on 11 September 2026 from OMP 18.1.17, pi 0.75.5, Claude Code 2.1.268,
Codex 0.154.0 and OpenCode 1.18.30. pi/OMP/OpenCode used a delayed local mock
chat-completions endpoint; Claude/Codex ran brief shell sleep commands.

The same busy frame is deliberately usable repeatedly: a static screen must
never be treated as proof of input readiness. Done frames prove the prompt
becomes eligible again after work finishes. These are terminal observations,
not agent-authored descriptions of state.

Direct-steering checks on 2026-09-11 added post-interrupt frames for pi, OMP,
OpenCode and Codex. Claude's `restored-input` frame contains the cancelled prompt
returned to the editor: it is deliberately not ready for submission. Its
`interrupted` frame follows Ctrl-U clearing that restored draft. pi/OMP/OpenCode
used the delayed local fixture; Claude and Codex used a shell-sleep prompt.
