# Company layout and protocol

[Project guide](../AGENTS.md)

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

The shared conventions require employee directories to live under `spaces/`,
including the CEO. They distinguish paths from an employee's starting directory
from paths at the company root, and require checking the recipient's existing
space before creating an inbox topic. These are prompt rules, not filesystem
enforcement.

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
