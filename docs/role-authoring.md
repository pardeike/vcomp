# Role authoring provenance

The original expanded profession catalogue was committed as `d2a7c4f` on
9 September 2026, titled "Add the Codeness role catalogue: 23 more positions,
same small default". Its co-author was Claude Opus 5 (1M context).

The local Claude history corroborates the model as `claude-opus-5` in session
`0c780c29-4b20-486c-a8d7-6f4000a6a2bb`, under the original `/private/tmp/vcomp`
project. The catalogue-writing command appears at 16:04:06 UTC and the commit
at 16:04:42 UTC. Later edits came from other sessions too; this does not attribute
every current line to Opus 5.

The relevant owner requests on that date were:

- 14:22 UTC: filesystem-only company communication, individual backstories,
  a delegating CEO and a deliberately simple engine.
- 15:59 UTC: inspect the Codeness and VirtuAgents roles and expand the catalogue
  while preserving a small default roster.
- 16:09 UTC: creative personal histories and productive personality friction.
- 16:19 UTC: compose each role from a personal backstory and a shared professional
  description; people of the same profession should still differ.

There was no single standalone generation prompt to copy. The shipped
`role_generation.md` reconstructs these requirements and uses actual existing
professional definitions as style examples. It asks for a concrete remit and a
useful bias with a blind spot, while leaving personal history and standing rules
out of the generated definition. It is an ordinary editable template.

The default authoring model is therefore `claude-opus-5` through Claude CLI,
with high effort. This choice governs catalogue writing only.
