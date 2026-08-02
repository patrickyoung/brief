# Development guidance

Rob Pike is the bar, and `ask` is the sibling. `brief` is smaller than `ask`
and should stay that way: it has no network code, no credentials, no
provider adapters and no session format of its own, because the one thing
it needs a model for is done by running the program that already does that.

The whole program is four steps: read the frontmatter of every skill on the
path, rank it against a task, print a name, print a body. If a change makes
that sentence longer, it needs a very good reason.

When changing `brief`:

- **preserve the disclosure invariant.** `find -ask` sends names and
  descriptions and nothing else. A body is read when a skill is named, a
  bundled file when its path is written down. This is the reason choosing
  costs a few hundred tokens instead of a context window, and the reason a
  private procedure stays on the machine holding it. It is asserted from
  the outside, against bytes recorded leaving the process, and that is the
  only place it can honestly be asserted from — do not weaken the test into
  one that reads the source;

- **preserve the exit contract**: 0 yes, 1 no, 2 error. It is `grep`'s, not
  `ask`'s, because `brief` asks a question where no is a real answer. A "no"
  must leave stdout empty, or a pipeline hands a diagnostic to `brief cat` as
  if it were a name;

- **a name is never a path.** Everything that resolves a reference goes
  through `validName` first, and a path inside a skill goes through
  `inside`. Both exist because `brief cat` is meant to be handed the output
  of `brief find -ask`, which is to say the output of a language model. A
  model naming a skill that does not exist is ordinary; a model naming one
  that `brief` then opens is a vulnerability;

- **never guess.** The ranking discards a word most of the catalogue uses
  rather than scoring it low, and refuses a match that explains one word of
  a five-word question, so a task made of common words or of one
  coincidence returns nothing. A confidently wrong skill is worse than no skill: the agent that
  loads it follows the wrong procedure and cannot tell. If a change makes
  `rank` return something for every input, it is a regression however good
  the top result looks;

- **do not hand-roll YAML.** A validator that disagrees with the parser the
  agent actually uses is worse than no validator, and frontmatter is the
  one place where being right about somebody else's file matters. The
  dependency is one library and it stays;

- **lint quotes the specification, and only the specification.** Every
  limit in `lint.go` is a number from the spec, named as a constant, tested
  against a literal. A rule `brief` invented is a rule that makes conforming
  skills look broken. Two rules are inferences rather than quotations — a
  reference that does not resolve, and a bundled file nothing mentions —
  and both are warnings for that reason;

- **errors are violations, warnings are advice.** A linter that fails a
  build over advice is a linter somebody turns off, which costs more than
  the advice was worth. `-strict` exists for the people who want the other
  bargain;

- **read a skill whole, print only what was asked for.** `brief` reads a
  `SKILL.md` from disk in one go because they are small and a partial read
  is a subtler bug than it is worth avoiding. What matters is not what is
  read but what is *sent* and what is *printed*, which is the invariant
  above;

- **never truncate.** A `SKILL.md` past the size bound is an error naming
  the bound. Half a procedure that reads like a whole one is the failure
  nothing downstream can detect;

- **keep `find -ask` out of the caller's conversation.** It runs `ask -n -f`
  into a session of `brief`'s own. `ask` continues by default, and a
  selection that landed in the current conversation would answer the next
  question with a catalogue on the model's mind. The session is kept, not
  deleted: a skill selection is a decision made on somebody's behalf, and
  `ask replay -check` is what makes it reviewable;

- run `go test ./...` — and `go test -race ./...` — before reporting
  success;

- **keep the docs true**: a new flag belongs in `brief help` and `brief.1`; a
  new command belongs in both plus `README.md`. Tests enforce each of
  those, plus a lint-clean pure-ASCII man page, help inside eighty columns,
  and the specification's numbers quoted correctly in both the code and the
  manual. Guard wholes as well as parts: a flag-level check stays green
  while an entire verb goes undocumented.

Things left out on purpose. Do not add them back without asking: installing
a skill, running one, a registry or marketplace client, a cache, a config
file, a daemon, an MCP server, an index format, a second way to name a
skill besides its directory, and any provider code at all. If `brief` needs
a model, it runs `ask`.

Two that arrive wearing a good reason, and are still the same thing:

- **counting how often a skill is chosen.** `hone` prints how many lessons
  a skill holds, and lessons-per-use would be the truer gauge — but a use
  counter is a file `brief` writes on every `find`, which is a cache, which
  is the entry above. `brief` reads the catalogue and prints; it has no
  state and a program with no state cannot corrupt any;
- **execution policy in frontmatter** — a `check:`, `tools:` or `model:`
  key, so `ply -s` could load a procedure and its contract together. It is
  the most attractive idea anyone brings here and it may one day be right,
  but it makes `brief` know how things are run, and it creates a second way
  to name a tool. A capability is already a file: a shell script that ends
  in `exec ply ...`, named, versioned, and on `$PATH`. Use that until it
  demonstrably hurts, and bring the evidence.
