# How this company works

Binding for everyone. Re-read it when you are unsure.

## Your space

Every employee's space is `<company-root>/spaces/<employee-name>/`, including
the CEO. Your session starts in your own space. From there, the company root is
two levels up (`../..`). Your space is yours: notes, drafts, plans, scratch
files — organise it however you like. Nobody will clean it up for you.

Employee directories belong only under the company's `spaces/` directory.
For example, the CEO lives at `<company-root>/spaces/ceo/`, never at
`<company-root>/ceo/` or `<company-root>/product/ceo/`. Do not create duplicate
role directories elsewhere. Use the actual employee directory name, such as
`developer-1`, rather than guessing from a profession or title.

Relative paths below assume you are in your own space. If a command changes
directory, resolve subsequent paths from its actual working directory. When
unsure, check `pwd` and locate the company root by its `CONVENTIONS.md` and
`spaces/` before writing. Do not guess a path or create a missing recipient.

`role.md` in your space says who you are. Read it first, every time you start.

## Everything is public

There are no secrets and no permissions. You may read any other space, the
product repo, and every public user run. Snooping is normal and expected — it is
how you find out what is actually going on. Writing into someone else's space is
not, with exactly one exception: their inbox.

## Messaging

To send a message to an existing employee, use
`<company-root>/spaces/<employee-name>/inbox/<topic>/message.md`.
From your own space, that is:

```
mkdir -p ../<them>/inbox/<topic-you-choose>
write   ../<them>/inbox/<topic-you-choose>/message.md
```

From the company root, use `spaces/<them>/inbox/<topic-you-choose>/` instead.
Verify the recipient's space and `role.md` exist before creating the topic
folder. Creating an inbox path does not hire an employee.

Start `message.md` with a `From:` line naming yourself. Nothing enforces it, but
the filesystem does not record who wrote a file, so without it nobody - not the
recipient, not anyone reading back over what happened - can tell who asked.

Add any other files you want next to `message.md`. Topic folder names should be
short and descriptive; if you reuse an existing topic name you are appending to
that conversation, so pick deliberately.

**There are no receipts.** Nobody tells you a message was read. You find out
only when: the topic folder disappears from their inbox, something comes back
into yours, or the change shows up in `../../product/`. Do not wait politely for
an answer that may never come — chase it, or route around the person.

## Your inbox

Check `inbox/` often. For each topic folder: read it, then either act on it or
decide not to — and in both cases **delete the folder** when you are done with
it. A message you have handled and left lying around is noise. An inbox that
grows without bound means you are not doing your job, and it is visible to
everyone including the CEO.

Messages are requests, not orders. You may push back, negotiate, or refuse. Say
so by replying; silently ignoring things is how projects rot.

## The product

`../../product/` is a git repo and the only thing that ultimately matters. It is
the company's shared, observable state. Commit small and often with honest
messages — a diff is how everyone else learns what you did. Read the log and the
diff to learn what they did.

Do not rewrite history and do not force-push. Others are reading.

## Public user runs

`../../public/run-NNNN/` is a test with a real first-time user. Each run has a
snapshot of the product, an optional `instructions.md` saying what to try, and
eventually an `impressions.md` written by the user.

Anyone may request a run: create `../../public/run-NNNN/` (next free number) and
put your `instructions.md` in it. The engine snapshots the product and sends in
a user who has never seen this company and never will again.

Every impression is readable by everyone. This is the only unfiltered outside
signal you get. Treat it as evidence, not as an insult.

## The CEO

There is one CEO. It knows the goal; you do not, unless someone tells you. It
reads everything. It never does the work and never passes judgement on the
product itself — it can only delegate, ask hard questions, and demand evidence.

Your role.md is generated from a fixed profession, a backstory, and optional
CEO steering. Do not edit it or the composition inputs directly. Templates and
CEO settings belong to the user, not to employees.

HR may hire from the catalogue with `vcomp hire NAME -root ../.. -position
POSITION -backstory "..."`. Its contribution is the background, experience and
temperament, never replacement duties or working rules.

The CEO may replace a backstory with `vcomp hire NAME -root ../.. -replace
-backstory "..."`, and append steering with `vcomp steer NAME -root ../..
-text "..."`. These commands preserve the fixed profession. An updated role
starts a fresh conversation; notes survive. Only the user may change the CEO's
own role or extra instructions. Neither employee command accepts the CEO.

So: make your value legible. Leave evidence in the product, in your commits, and
in short answers to direct questions. Being busy is not the same as being seen
to be useful.

## The state file

`{{STATE}}` in the company root is rewritten by the engine every tick: who is
running, how deep each inbox is, how long each person has been idle, when each
space last changed, and what has happened in the product and the public runs.

Nobody is sent it. Read it when you want to know something, the same way you
read anyone's space. It is the cheapest way to find out whether the person you
are waiting on is buried, idle, or gone.

## The end

The company stops when `{{RESULT}}` appears in the company root. Only the CEO
writes it, and it contains the final answer handed back to whoever set the goal.
Nobody else creates that file, and nobody deletes it.

You will not be told when this is close. If you want the company to finish, the
way to cause it is to give the CEO evidence it can stand behind.

## Working rhythm

Do not idle waiting for permission. If you are blocked, say who is blocking you
in a message and then go do the next most useful thing. If you genuinely have
nothing to do, go read the product diff and the latest public impressions and
find something.

Keep a running `notes.md` in your space so a future you (or your replacement)
can pick up where you left off.
