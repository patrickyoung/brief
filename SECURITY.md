# Security

## Reporting

Report a vulnerability privately through GitHub's
[security advisory form](https://github.com/patrickyoung/brief/security/advisories/new).
Please do not open a public issue for anything exploitable. Expect an
acknowledgement within a few days.

## What brief holds

Nothing. `brief` has no credentials, opens no ports, and stores no state
except one thing: the `ask` session behind each `brief find -ask` choice,
under `~/.brief/find/` (or `$BRIEF_DIR`). Those files hold the catalogue that
was sent and the name that came back — never a skill body — and they are
written by `ask`, which creates its session files 0600 in a 0700 directory.

`brief` reads skills from `$BRIEF_PATH` and prints them. It never writes to a
skill except through `brief new`, which refuses to overwrite without `-f`.

## Deliberate properties

- **A name is never a path.** A skill name may hold only lowercase letters,
  digits and single hyphens, so it cannot be `../x`, `/etc/passwd`, or
  `-rf`. Every reference goes through that check before anything is opened.
- **A path inside a skill cannot leave it.** `pdf/../../etc/passwd` is
  refused by the resolver, not by the filesystem's permissions and not by
  hoping. This matters because `brief cat` is designed to be handed the
  output of `brief find -ask`, which is the output of a language model.
- **The model's answer is checked against the catalogue.** A model that
  invents a plausible skill name is an ordinary event. `brief` matches what
  comes back against the names it offered and reports an error rather than
  opening anything else.
- **`find -ask` sends the catalogue and nothing else.** No skill body, no
  script, no bundled file leaves the machine. If a skill's instructions are
  confidential, choosing between it and nine others does not disclose them.
  A test asserts this against the bytes recorded leaving the process.
- **Nothing is executed.** `scripts/` is listed and printed, never run.
  `brief` has no code path that executes anything except the one program it
  is designed to run, `ask`, named by `$ASK`.
- **Symbolic links are listed, not followed.** Walking them would turn a
  catalogue into an unbounded search of a filesystem somebody else laid
  out.
- **A `SKILL.md` past the size bound is an error, never a truncation.**

## Not in scope

**A skill is somebody else's instructions to your agent.** That is what a
skill is for, and `brief cat` prints one without judgement. Installing a
skill from a stranger is exactly as consequential as installing a program
from a stranger, and rather less visible, because the instructions are read
by a model rather than by you. `brief lint` will tell you whether it
conforms to the specification. It has nothing to say about whether it is
honest. Read skills before you install them, and treat `references/` and
`scripts/` as code.

Prompt injection through a skill body, or through content a skill tells an
agent to read, is inherent to the task. `brief` moves text; what an agent
does with that text is downstream of `brief` and of the agent.
