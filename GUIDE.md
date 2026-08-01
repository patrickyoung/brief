# The field guide

What `lore` is good at, what it is not, and the recipes — measured against
a real machine with nine skills installed and a real `ask` account.

---

## 1. The idea, in one paragraph

A skill is procedural knowledge someone wrote down: a directory with a
`SKILL.md` in it. An agent is supposed to load the right one at the right
moment and no others. Every product that supports skills builds that
selection into itself, which means the knowledge is only usable inside that
product. `lore` takes the other route: it makes the catalogue a filter, so
selecting a skill is a shell command and using one is a pipe. What you get
back is portable — the same `lore cat` output is a system prompt for `ask`,
an `--append-system-prompt` for Claude Code, or a paste into anything else.

## 2. What it is good at

- **Turning a task into the right instructions, cheaply.** Nine skills are
  3.7 KB as a catalogue and 88 KB as instructions. Choosing reads the small
  number.
- **Composing.** `lore cat a b` concatenates two skills. `ask system` then
  a skill is a system prompt with both. Nothing here is a plugin format.
- **Telling you when a skill is broken.** `lore lint` finds the failures
  that are otherwise silent: a name that does not match its directory, a
  `references/` file the instructions promise and nobody shipped.
- **Being scriptable.** Names on stdout, one per line. `xargs`, `cut`,
  `while read` — all of it works, because there is nothing else on stdout.

## 3. What it is not

- **Not an agent.** It never runs a script from `scripts/`, and never will.
  If a skill says "run `scripts/extract.py`", something with tools has to
  do that. `ask` has no tools. See §8.
- **Not a package manager.** There is no `lore install`. A skill is a
  directory; `git clone` it where `$LORE_PATH` looks.
- **Not smart by default.** `lore find` matches words. It is right about
  most of what people type and honest about the rest — it prints nothing
  rather than guessing. `-ask` is the flag for the rest.

## 4. The pair

The whole integration is that `lore` prints text and `ask -S` takes text.

```sh
ask -S "$(ask system; lore cat pdf-processing)" "pull the tables from q3.pdf"
```

`ask system` first, then the skill. `ask -S` *replaces* the default system
prompt, and the default is what makes `ask` a filter — answer and stop, no
preamble, take the most useful reading rather than asking a question. A
skill is a procedure, not a personality; you want both.

### The function you actually want

```sh
skilled() {
  local s
  s=$(lore find -ask -q "$*") || { ask "$@"; return; }
  ask -S "$(ask system; lore cat "$s")" "$@"
}
```

```
$ git diff --cached | skilled "write a commit message"
```

The `||` is the whole design in one character: `lore find` exits 1 when
nothing fits, and "nothing fits" means *ask the model normally*, not *fail*.
A skill loaded for a task it does not suit is worse than no skill.

One thing to know about that function: `ask` continues the current
conversation by default, so `skilled` attaches a skill to whatever was
already being discussed. Usually that is what you want — you asked three
questions and now you want the fourth answered with a procedure in hand.
When it is not, add `-n`:

```sh
skilled() {
  local s
  s=$(lore find -ask -q "$*") || { ask -n "$@"; return; }
  ask -n -S "$(ask system; lore cat "$s")" "$@"
}
```

`lore find -ask` never has this problem: it runs in a session of its own
(§6), so choosing a skill is never a turn in your conversation.

### It works with more than one

```sh
ask -S "$(ask system; lore cat house-style commit-message)" "..."
```

`cat` is `cat`. Order matters only as much as it does in any prompt.

## 5. Choosing: the two mechanisms, and when each fails

```
$ lore find "durable object websocket chat room"
durable-objects
```

Five of those run in 18 ms total. No network, no key, no account. The
ranking weights each word by how rare it is across the installed
catalogue, gives a skill's own name triple weight, and **discards any word
most of the catalogue uses** rather than scoring it low. `-v` shows the
work:

```
$ lore find -v -n 3 "review my worker for bad practices"
lore: workers-best-practices     5.86  review practice
lore: durable-objects            3.09  review practice
lore: wrangler                   1.39  practice
workers-best-practices
durable-objects
wrangler
```

Here is where it fails, and it is worth seeing:

```
$ lore find "make my website load faster"
$ echo $?
1
```

Nothing. The skill that fits is `web-perf`, whose description reads
"Analyzes web performance… Core Web Vitals (LCP, INP, CLS)". It shares no
distinctive word with the task. A model closes that gap in about a second:

```
$ lore find -ask "make my website load faster"
lore: web-perf · ask replay -check ~/.lore/find/20260801-205940-a0b7e2fa.jsonl
web-perf
```

**Use words in a loop, a model at a prompt.** Or both, which is the honest
default for a script that runs often:

```sh
s=$(lore find "$task") || s=$(lore find -ask -q "$task")
```

Free when it can be, paid when it has to be.

### Which model chooses

`-ask` inherits `$ASK_MODEL`. Selection is a classification task and does
not want your best model:

```sh
export LORE_MODEL=anthropic/claude-haiku-4-5-20251001
```

`$ASK` points at the binary, so a wrapper that pins anything at all works
too.

## 6. Every choice is replayable

`-ask` never touches the conversation you are having. It runs `ask -n -f`
into a session of `lore`'s own, and says which one:

```
$ lore find -ask "the input is a patch that needs a message"
lore: commit-message · ask replay -check ~/.lore/find/20260801-211412-e5f11f65.jsonl
commit-message

$ ask replay -check ~/.lore/find/20260801-211412-e5f11f65.jsonl
ok: 20260801-211412-e5f11f65.jsonl replays exactly (5 events)
```

That matters more than it looks. Skill selection is a decision made on your
behalf that changes what an agent does next, and it is normally invisible.
Here it is a file: which catalogue was offered, which name came back, what
it cost. Months later `ask replay` will still render it, and `-check` will
still prove it was not edited.

Those files accumulate, one small JSONL per `-ask`. `rm -rf ~/.lore/find`
whenever you like; nothing depends on them.

## 7. A worked example, start to finish

```sh
$ cat ~/.lore/skills/commit-message/SKILL.md
---
name: commit-message
description: Writes a git commit message from a diff. Use when a change needs
  a message, or when the input is a patch or a diff.
---

# Commit messages

Read the diff. Write the message and nothing else.

## Rules

- A subject line in the imperative mood, under 50 characters, no full stop.
- Then a blank line, then one paragraph on why, only if the why is not obvious.
- Never describe the diff line by line. Say what changed and what it is for.
```

```sh
$ git show HEAD --stat | ask -q -S "$(ask system; lore cat commit-message)" \
    "write the commit message"
Remove links to the private mu repository

Avoid directing README readers to inaccessible 404 pages.
```

Under 50 characters, imperative, blank line, one paragraph of why. The
rules came from a file on disk that anybody on the team can edit, and the
model never saw the other eight skills.

## 8. Skills assume tools. `ask` has none.

This is the one real seam in the pair, and it is better to know it now.

Many published skills are written for an agent with a filesystem and a
shell: "run `scripts/extract.py`", "read `references/FORMS.md` first". `ask`
has no tools, so those instructions describe things it cannot do.

Three ways through it, in order of how often you will want them:

**Inline the level-three file yourself.** This is what an agent's tool call
would have fetched anyway, and it is one more `cat`:

```sh
ask -S "$(ask system; lore cat pdf-processing pdf-processing/references/FORMS.md)" \
    "which fields does this form have?" -a scan.pdf
```

**Prefer knowledge skills for `ask`.** House style, a review checklist, an
output format, a domain glossary — procedures whose whole content is
judgement. They work perfectly, because the judgement is the deliverable.

**Give the skill to something with tools.** `lore` prints text; anything
that accepts a system prompt accepts it:

```sh
claude --append-system-prompt "$(lore cat turnstile-spin)" -p "add a captcha"
codex exec "$(lore cat wrangler)

Now deploy the worker."
```

That is the payoff for `lore` not being an agent: the same catalogue serves
`ask`, Claude Code, and whatever you use next year.

## 9. Writing a skill that actually gets chosen

Selection runs on the description. Everything else in the file is invisible
until after the choice is made — which means **the description is the
skill**, as far as being found is concerned.

- **Say what and when.** "Extracts tables from PDFs" is what. "Use when a
  task involves a PDF, a scan, or a form" is when. `lore lint` warns when
  the second half is missing, because it is the half selection runs on.
- **Write the words a user would use, not the words you would.** A user
  says "my page is slow", not "Core Web Vitals". Put both in.
- **Do not pad.** Every agent loads every description at startup, for every
  installed skill, forever. `lore lint` warns past 600 characters for that
  reason, and the specification caps it at 1024.
- **Test it.** This is the part nobody does, and `lore` makes it one line:

```sh
for t in "my page is slow" "why is the site sluggish" "improve LCP"; do
  printf '%-28s %s\n' "$t" "$(lore find "$t" || echo MISS)"
done
```

If your own skill misses the phrasings you expect, fix the description, not
the ranking.

## 10. Keeping a catalogue honest

```
$ lore lint
~/.claude/skills/cloudflare/SKILL.md:4: warning: unknown field "references"; …
~/.claude/skills/cloudflare/SKILL.md:11: warning: 320 file(s) in references/ …
~/.claude/skills/durable-objects/SKILL.md:5: warning: 3 file(s) in references/ …
~/.claude/skills/turnstile-spin/SKILL.md:4: warning: unknown field "references"; …
~/.claude/skills/turnstile-spin/SKILL.md:12: warning: 6 file(s) in references/ …
~/.claude/skills/wrangler/SKILL.md:5: warning: the body is 919 lines (the
  specification asks for under 500); move detail into references/
lore: 9 skill(s), 0 error(s), 6 warning(s)
```

(Lines are one per finding and unwrapped; they are shortened here to fit
the page.)

Those are real findings against real published skills. The middle one is
the interesting kind: 320 reference files that no instruction in `SKILL.md`
names. Progressive disclosure means an agent opens a file when the
instructions tell it to. Nothing tells it to.

In CI, one line:

```yaml
- run: lore lint -strict .claude/skills
```

In a pre-commit hook, the same line with `-q`, because the exit status is
the whole message.

Errors — things that will actually break — are worth knowing by sight:

| finding | what happens without lint |
| --- | --- |
| name is not the directory name | the skill silently never loads |
| duplicate key | YAML keeps the first; your edit did nothing |
| description over 1024 characters | rejected by a conforming loader |
| `references/GONE.md` is not there | fails only when the skill is used |
| empty body | the skill loads and says nothing |

## 11. Three things that will bite you

**Shadowing is silent.** `$LORE_PATH` is `$PATH`: `.claude/skills` in the
project shadows `~/.claude/skills`. A skill you edited in your home
directory and cannot see any effect from is the classic symptom.

```
$ lore path -a cloudflare
/Users/you/work/api/.claude/skills/cloudflare
/Users/you/.claude/skills/cloudflare
```

**`lore find` says nothing rather than guessing, so unguarded substitution
runs the wrong command.** `$(lore find "$t")` can be empty, and
`lore cat ""` is an error, and `ask -S "" ...` is a raw model with no
system prompt at all. Always guard:

```sh
s=$(lore find -ask "$t") || { echo "no skill" >&2; exit 1; }
```

`xargs` also does the right thing here: given no input it runs nothing,
which is exactly the behaviour you want and the reason `lore find` prints
nothing rather than a diagnostic when the answer is no.

**`-ask` costs a call and about a second.** Over a hundred tasks that is a
hundred calls and two minutes of wall clock. Rank first, fall back second
(§5), and remember that `lore ls` is only read once — if you are looping,
hoist the catalogue and choose in one batch:

```sh
lore ls | ask -q "For each task below, name the one skill that fits or none.
One line per task, in order, name only.

$(cat tasks.txt)"
```

That is one call for a hundred tasks. `lore find -ask` is the careful
version for one task; the shell is right there for the other shape.

---

## Appendix: the exit codes, again

| | |
| --- | --- |
| 0 | yes — found, listed, clean |
| 1 | no — nothing matched, or lint had something to say |
| 2 | error — bad usage, unreadable skill, `ask` failed |

`ask` uses 1 for error and 2 for a full context window. `lore` uses `grep`'s
convention instead, because "no skill fits" is an answer and has to be
distinguishable from "something is broken". The one place they meet is
`skilled()` in §4: `lore find` returning 1 falls back to a plain `ask`, and
returning 2 should not.
