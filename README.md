# brief

Find the skill that fits a task. Print it on stdout.

```
$ brief find "my page is slow"
web-perf

$ ask -S "$(brief cat web-perf)" "why is this slow?" < trace.json

$ ask -S "$(ask system; brief cat pdf-processing)" "pull the tables from q3.pdf"
```

A skill is a directory with a `SKILL.md` in it: YAML frontmatter saying what
it does and when to use it, then Markdown instructions. That is the [Agent
Skills specification][spec], and it is all `brief` knows. It does not run
skills, install them, or wrap an agent around them. It is the catalogue, as
a filter — it lists, it chooses, it prints, and it stops there.

[`ask`][ask] is the companion, and the name is the whole idea: **you brief
the agent, then you ask it.** `ask` has the model, `brief` has the
procedure, and the seam between them is a pipe.

[spec]: https://agentskills.io/specification
[ask]: https://github.com/patrickyoung/ask

## Install

```
go install github.com/patrickyoung/brief@latest
```

Go 1.26 or newer, and a Unix. Nothing to configure: `brief` reads the skills
you already have. If you use Claude Code, that is `~/.claude/skills` and
`.claude/skills` in the project, and both are on the default path.

`ask` is optional. Everything except `brief find -ask` works without it, and
without a network, a key, or an account.

## Progressive disclosure is a pipeline

The specification asks agents to load skills in three stages: the names and
descriptions at startup, one skill's instructions when it is chosen, and its
bundled files only when they are needed. That is not a runtime. It is `ls`
and `cat`.

| level | the specification says | `brief` |
| --- | --- | --- |
| 1 | name and description, ~100 tokens each, always loaded | `brief ls` |
| 2 | the instructions, under 5k tokens, when the skill is chosen | `brief cat name` |
| 3 | scripts, references and assets, only when needed | `brief ls name`, `brief cat name/references/FORMS.md` |

So the whole mechanism is two programs and a pipe:

```
$ brief ls
agents-sdk	Build AI agents on Cloudflare Workers using the Agents SDK. Load when …
cloudflare	Comprehensive Cloudflare platform skill covering Workers, Pages, storage …
durable-objects	Create and review Cloudflare Durable Objects. Use when building …
…

$ brief ls | ask -q 'which of these fits "my page is slow"? name only'
web-perf
```

`brief find -ask` is that pipeline with the failure modes handled, and the
numbers are the reason it exists: those nine skills are **3.7 KB** as a
catalogue and **88 KB** as instructions. Choosing costs about 900 tokens
instead of 22,000, and 21,000 of those tokens would have been about the
eight skills that were wrong.

## The two ways to choose

```
$ brief find "durable object websocket chat room"     # words: free, instant, offline
durable-objects

$ brief find "make my website load faster"            # nothing matched
$ echo $?
1

$ brief find -ask "make my website load faster"       # a model, via ask
brief: web-perf · ask replay -check ~/.brief/find/20260801-205940-a0b7e2fa.jsonl
web-perf
```

The default is a weighted word match over names and descriptions. A word
only one skill uses decides; a word most of them use is dropped, not scored
low — which is why the second command above says nothing rather than naming
whichever skill sorted first. **A confidently wrong skill is worse than no
skill**, because the agent that loads it is then following the wrong
procedure with no way to tell.

`-ask` runs `ask` for the ones words cannot reach. "Make my website load
faster" shares no word with "Measures Core Web Vitals (LCP, INP, CLS)", and
a model closes that gap for about a tenth of a cent.

Three things hold that flag together:

- **The disclosure invariant.** `-ask` sends the catalogue and the task.
  Never a body, never a script, never a bundled file. The catalogue is a
  private, stable-named text attachment containing exactly the bytes `brief
  ls` prints; the task is user text on Ask's stdin. A test records both.
  Task text therefore appears in neither process argv nor the higher-priority
  selector system prompt.
- **Every choice is replayable.** `-ask` runs `ask` in a fresh session of
  its own under `~/.brief/find/`, never the conversation you are having, and
  prints where it went. `ask replay -check` proves that session months
  later. A skill selection is a decision an agent made on your behalf, and
  it should be possible to read it back.
- **The answer is checked, not trusted.** A model that invents a plausible
  skill name is an ordinary event. That name is about to become a path, so
  it is matched against the catalogue and refused if it is not there.

## The Unix contract

| stream | carries |
| --- | --- |
| stdout | names, instructions, findings — the answer, and nothing else |
| stderr | which session recorded a choice, why a ranking went that way |
| exit 0 | yes: found, or clean |
| exit 1 | no: nothing matched, or lint had something to say |
| exit 2 | error: bad usage, unreadable skill, `ask` failed |

Yes and no are both ordinary answers a script branches on, so they are
separated from *broken*:

```sh
s=$(brief find -ask "$task") || { echo "no skill for that" >&2; exit 1; }
ask -S "$(ask system; brief cat "$s")" "$task"
```

This is grep's contract, not `ask`'s. `ask` answers a question, where
anything other than an answer is a failure; `brief` asks one, and "no" is a
real answer. When they are composed, the difference matters exactly once —
`brief find` returning 1 means nothing fit, and should not be retried.

## Where skills live

`$BRIEF_PATH` is `$PATH`, for skills: colon-separated, searched left to
right, first match wins. The default is

```
.claude/skills : ~/.claude/skills : ~/.brief/skills
```

so a skill in the project shadows the one in your home directory, the same
way `./bin/foo` shadows `/usr/bin/foo`. Shadowing is silent by design and
visible on request:

```
$ brief path -a cloudflare
/Users/you/work/api/.claude/skills/cloudflare
/Users/you/.claude/skills/cloudflare
```

A skill can also be named by path, which is how you use one that is not
installed anywhere yet: `brief cat ./draft`, `brief lint .`.

## lint

The specification has rules with numbers in them, and a skill that breaks
one usually fails silently — it loads with the wrong name, or it does not
load at all, and nothing says why.

```
$ brief lint
~/.claude/skills/cloudflare/SKILL.md:4: warning: unknown field "references"; …
~/.claude/skills/cloudflare/SKILL.md:11: warning: 320 file(s) in references/ …
~/.claude/skills/wrangler/SKILL.md:5: warning: the body is 919 lines (the
  specification asks for under 500); move detail into references/
brief: 9 skill(s), 0 error(s), 6 warning(s)
```

Errors are violations: a name that is not the directory name (the skill will
not load, and nothing will tell you), a description over 1024 characters, a
`metadata` that is not a mapping, a duplicate key that YAML silently drops.
Warnings are the specification's advice and its silences — the 500-line
budget, a description that says what a skill does but never when to use it,
and the two that only show up in production:

- **a reference that is not there.** `SKILL.md` tells the agent to read
  `references/FORMS.md`, and there is no such file. Progressive disclosure
  makes this invisible until the moment the skill is used.
- **a file nothing mentions.** Twelve files in `references/` that no
  instruction names. No agent reads a directory it was never told about, so
  they are not disclosed progressively — they are not disclosed at all.

`-strict` promotes warnings to errors, which is what CI wants. `-q` prints
nothing and leaves the exit status, which is what a hook wants.

## Writing one

```
$ brief new pdf-processing
pdf-processing/SKILL.md
$ brief lint -strict pdf-processing
brief: 1 skill(s), 0 error(s), 0 warning(s)
```

The scaffold passes `-strict` on the way out, because the first file an
author sees teaches them the shape.

## The prompt is a value

`brief prompt` prints the system prompt `find -ask` sends, so extending it is
ordinary shell rather than a fork:

```
$ brief prompt | tail -1
none is a real answer and often the right one. A skill loaded for a task it
does not fit costs the agent its context and points it at the wrong
procedure, which is worse than no skill at all.
```

`brief help` prints every command and flag inside eighty columns, and
`brief version` prints one number. Both go to stdout, so `brief help | less`
works and a misuse still leaves stdout empty for whatever was parsing it.

## Deliberately absent

No install command, no registry, no marketplace, no cache, no config file,
no daemon, no MCP server, and no way to execute a skill. `brief` prints
things. Running what it prints is the shell's job, and there is already a
program for the part that needs a model.

**[GUIDE.md](GUIDE.md)** has the recipes: the shell function that turns any
task into a skilled `ask`, using `brief` with agents that are not `ask`,
keeping a project's skills honest in CI, and the three things that will bite
you.
