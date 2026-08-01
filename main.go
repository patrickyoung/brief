// Command lore finds the skill that fits a task and prints it on stdout.
//
// A skill is a directory holding a SKILL.md — YAML frontmatter saying what
// it does and when to use it, then Markdown instructions — as laid out by
// the Agent Skills specification. lore is the catalogue of them, as a
// filter: it lists, it chooses, it prints, and it stops there. Nothing
// here runs a skill, because a filter that also executed things would be
// an agent, and there is already one of those on the other end of the pipe.
//
// It is the companion to ask, and the seam between them is a pipe:
//
//	ask -S "$(ask system; lore cat pdf-processing)" "pull the tables"
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const version = "0.1.0"

// The exit contract, in one place. It is grep's, not ask's: lore answers
// questions with a yes or a no, and both are ordinary outcomes a script
// branches on. Something being broken is the third case and gets its own
// code, so `lore find x || fallback` cannot be silently triggered by an
// unreadable directory.
const (
	exitYes = 0
	exitNo  = 1
	exitErr = 2
)

const usageText = `lore — find the skill that fits a task, and print it on stdout

  lore ls [ref]             list every skill, or the files inside one
  lore cat ref...           print a skill's instructions, or one of its files
  lore find [flags] task    name the skill that fits a task
  lore lint [flags] [path]  check skills against the Agent Skills spec
  lore new [flags] name     write a new skill that already passes lint
  lore path [flags] name    print the directory a skill lives in
  lore prompt [flags]       print the system prompt find -ask sends
  lore version              print the version (-V, --version)
  lore help                 print this summary (-h, --help)

a skill is a directory with a SKILL.md in it: frontmatter saying what it
does and when to use it, then the instructions. A ref is a skill's name, or
a name and a path inside it — pdf-processing/references/FORMS.md. Names
resolve on $LORE_PATH left to right, first match wins, exactly like $PATH.

progressive disclosure is a pipeline, not a runtime. ls is level 1: a name
and a description, a few hundred tokens for the whole catalogue. cat is
level 2: the instructions for the one skill you named. cat of a ref inside
a skill is level 3. Nothing reads a level nobody asked for, and find -ask
sends level 1 and never anything else.

the pair: lore chooses and prints, ask answers.
  ask -S "$(ask system; lore cat web-perf)" "why is this slow?" < trace.json
  ask -S "$(ask system; lore cat $(lore find -ask "$t"))" "$t"

flags:
  find:  -ask       choose with a model by running ask, not by matching words
         -n num     print at most num names, best first (default 1)
         -v         explain the ranking on stderr
         -q         no progress on stderr
  lint:  -strict    treat warnings as errors
         -q         print nothing; the exit status is the answer
  new:   -d dir     create the skill under dir (default: here)
         -f         overwrite an existing SKILL.md
  path:  -a         print every directory holding the name, not just the first
  prompt: -n num    the limit the prompt states (default 1)

env: LORE_PATH  where skills live (default .claude/skills, ~/.claude/skills,
       ~/.lore/skills), searched left to right
     LORE_MODEL the model find -ask uses, when it should not be $ASK_MODEL
     LORE_DIR   where find -ask keeps its ask sessions (default ~/.lore)
     ASK        the ask binary to run (default: ask, found on $PATH)
exit: 0 yes · 1 no — nothing matched, or lint had something to say · 2 error
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return exitErr
	}
	switch args[0] {
	case "ls", "list":
		return cmdLs(args[1:])
	case "cat":
		return cmdCat(args[1:])
	case "find":
		return cmdFind(args[1:])
	case "lint", "check":
		return cmdLint(args[1:])
	case "new":
		return cmdNew(args[1:])
	case "path":
		return cmdPath(args[1:])
	case "prompt":
		return cmdPrompt(args[1:])
	case "version", "-V", "--version":
		fmt.Printf("lore %s\n", version)
		return exitYes
	case "help", "-h", "--help":
		fmt.Print(usageText)
		return exitYes
	}
	fmt.Fprintf(os.Stderr, "lore: unknown command %q\n", args[0])
	// The most likely mistake is naming a skill where a verb goes, and the
	// answer to it is one word long. Saying so beats printing the whole
	// usage at somebody who was one word away.
	if len(providers(args[0])) > 0 {
		fmt.Fprintf(os.Stderr, "lore: did you mean: lore cat %s\n", args[0])
	} else {
		fmt.Fprintln(os.Stderr, "lore: commands are ls, cat, find, lint, new, path, prompt, version, help")
	}
	return exitErr
}

// cmdLs prints the catalogue, or the files one skill bundles. Both are
// listings, so both are ls: the first is what an agent loads at startup,
// the second is what it can reach for afterwards.
func cmdLs(args []string) int {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	usage(fs, "lore ls [ref]")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	if fs.NArg() > 1 {
		return usageErr(fs)
	}
	if fs.NArg() == 1 {
		dir, rel, err := resolve(fs.Arg(0))
		if err != nil {
			return fail(err)
		}
		files, err := contents(filepath.Join(dir, rel))
		if err != nil {
			return fail(err)
		}
		for _, f := range files {
			fmt.Println(filepath.ToSlash(filepath.Join(rel, f)))
		}
		return found(len(files) > 0)
	}
	cat, err := catalogue()
	if err != nil {
		return fail(err)
	}
	// One skill per line, name then tab then description: the shape cut(1)
	// reads, and the exact bytes find -ask puts in front of a model.
	for _, e := range cat {
		fmt.Printf("%s\t%s\n", e.name, e.desc)
	}
	return found(len(cat) > 0)
}

// cmdCat prints instructions. A bare name gives the body with the
// frontmatter stripped — what belongs in a prompt, since name and
// description are how the skill was chosen and repeating them spends
// context the instructions are about to need. Naming the file instead
// (ref/SKILL.md) gives the file, byte for byte.
func cmdCat(args []string) int {
	fs := flag.NewFlagSet("cat", flag.ContinueOnError)
	usage(fs, "lore cat ref...")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	if fs.NArg() == 0 {
		return usageErr(fs)
	}
	// Every reference is resolved before anything is printed. Half a
	// composed prompt, followed by an error on a stream nobody was reading,
	// is a skill that silently lost its second half — and the shell has
	// already put it in a variable by then.
	type piece struct{ dir, rel string }
	var pieces []piece
	for _, ref := range fs.Args() {
		dir, rel, err := resolve(ref)
		if err != nil {
			return fail(err)
		}
		pieces = append(pieces, piece{dir, rel})
	}
	for _, p := range pieces {
		dir, rel := p.dir, p.rel
		if rel == "" {
			s, err := readSkill(dir)
			if err != nil {
				return fail(err)
			}
			body := s.instructions()
			if body == "" {
				return fail(fmt.Errorf("%s: no instructions after the frontmatter", s.path()))
			}
			fmt.Print(body)
			continue
		}
		f, err := os.Open(filepath.Join(dir, rel))
		if err != nil {
			return fail(err)
		}
		_, err = io.Copy(os.Stdout, f)
		f.Close()
		if err != nil {
			return fail(err)
		}
	}
	return exitYes
}

// cmdFind names the skill for a task. Words by default; a model with -ask.
// The two are one verb because they answer the same question and print the
// same thing — a name, on stdout, ready for cat — and differ only in what
// they cost and what they can see.
func cmdFind(args []string) int {
	fs := flag.NewFlagSet("find", flag.ContinueOnError)
	var (
		useAsk  = fs.Bool("ask", false, "choose with a model by running ask")
		n       = fs.Int("n", 1, "print at most this many names")
		verbose = fs.Bool("v", false, "explain the ranking on stderr")
		quiet   = fs.Bool("q", false, "no progress on stderr")
	)
	usage(fs, "lore find [flags] task...")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	if *n < 1 {
		return fail(errors.New("-n must be at least 1"))
	}
	task, err := taskFrom(fs.Args())
	if err != nil {
		return fail(err)
	}
	if task == "" {
		return usageErr(fs)
	}
	cat, err := catalogue()
	if err != nil {
		return fail(err)
	}
	if len(cat) == 0 {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "lore: no skills on %s\n", strings.Join(lorePath(), string(os.PathListSeparator)))
		}
		return exitNo
	}

	if *useAsk {
		names, err := askPick(cat, task, *n, *quiet)
		if err != nil {
			return fail(err)
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return found(len(names) > 0)
	}
	hits := rank(cat, task)
	if *verbose {
		for _, h := range hits {
			fmt.Fprintf(os.Stderr, "lore: %-24s %6.2f  %s\n", h.name, h.score, strings.Join(h.hits, " "))
		}
	}
	for i, h := range hits {
		if i == *n {
			break
		}
		fmt.Println(h.name)
	}
	return found(len(hits) > 0)
}

// cmdLint checks skills against the specification. With no argument it
// checks everything on the path, which is the question worth asking
// habitually: is anything I have installed quietly broken?
func cmdLint(args []string) int {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	var (
		strict = fs.Bool("strict", false, "treat warnings as errors")
		quiet  = fs.Bool("q", false, "print nothing; the exit status is the answer")
	)
	usage(fs, "lore lint [flags] [path...]")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	targets, err := lintTargets(fs.Args())
	if err != nil {
		return fail(err)
	}
	if len(targets) == 0 {
		if !*quiet {
			fmt.Fprintln(os.Stderr, "lore: no skills to check")
		}
		return exitYes
	}
	var errs, warns int
	for _, dir := range targets {
		for _, p := range check(dir) {
			if p.warn {
				warns++
			} else {
				errs++
			}
			if !*quiet {
				fmt.Println(p)
			}
		}
	}
	if !*quiet {
		fmt.Fprintf(os.Stderr, "lore: %d skill(s), %d error(s), %d warning(s)\n", len(targets), errs, warns)
	}
	return found(errs == 0 && !(*strict && warns > 0))
}

// lintTargets expands the arguments into skill directories. A directory
// that is a skill is checked; a directory of skills has each of them
// checked, which is what makes `lore lint ~/.claude/skills` mean what a
// person expects it to mean.
func lintTargets(args []string) ([]string, error) {
	if len(args) == 0 {
		cat, err := catalogue()
		if err != nil {
			return nil, err
		}
		var dirs []string
		for _, e := range cat {
			dirs = append(dirs, e.dir)
		}
		return dirs, nil
	}
	var dirs []string
	for _, a := range args {
		if !isDir(a) {
			// A file argument is almost always the SKILL.md itself.
			d, _, err := locate(a)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, d)
			continue
		}
		if _, err := os.Stat(filepath.Join(a, "SKILL.md")); err == nil {
			dirs = append(dirs, a)
			continue
		}
		ents, err := os.ReadDir(a)
		if err != nil {
			return nil, err
		}
		var found bool
		for _, de := range ents {
			sub := filepath.Join(a, de.Name())
			if strings.HasPrefix(de.Name(), ".") || !isDir(sub) {
				continue
			}
			if _, err := os.Stat(filepath.Join(sub, "SKILL.md")); err == nil {
				dirs = append(dirs, sub)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("%s: no SKILL.md, and no skill directories under it", a)
		}
	}
	return dirs, nil
}

func cmdPath(args []string) int {
	fs := flag.NewFlagSet("path", flag.ContinueOnError)
	all := fs.Bool("a", false, "print every directory holding the name")
	usage(fs, "lore path [flags] name")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	if fs.NArg() != 1 {
		return usageErr(fs)
	}
	dirs := providers(fs.Arg(0))
	if len(dirs) == 0 {
		// Not on the path is a "no", not a failure — but a directory sitting
		// right there under a name lore cannot resolve is worth saying out
		// loud, because it looks installed and is not.
		if dir, _, err := resolve(fs.Arg(0)); err == nil {
			fmt.Println(dir)
			return exitYes
		}
		return exitNo
	}
	if !*all {
		dirs = dirs[:1]
	}
	for _, d := range dirs {
		fmt.Println(d)
	}
	return exitYes
}

func cmdPrompt(args []string) int {
	fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
	n := fs.Int("n", 1, "the limit the prompt states")
	usage(fs, "lore prompt [flags]")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	if *n < 1 {
		return fail(errors.New("-n must be at least 1"))
	}
	fmt.Println(selectPrompt(*n))
	return exitYes
}

// maxTask bounds a task read from stdin. It is generous for a sentence and
// meaningless for a file, which is the point: a task is a description of
// work, and a program that quietly used the first megabyte of a log as one
// would answer a question nobody asked.
const maxTask = 1 << 20

// taskFrom takes the task from argv, or from stdin when argv is empty, so
// lore sits in a pipeline: `git log -1 | lore find -ask`.
//
// Argv wins outright rather than composing with stdin, which is where ask
// and lore differ on purpose. ask has two roles for its input — the
// instruction and the evidence — and needs both. A task is one phrase,
// there is nothing for a second source to be, and reading a stdin nobody
// meant to send would hang `find` inside any loop that inherited a pipe.
// A command that waits forever is a worse answer than a narrower one.
func taskFrom(args []string) (string, error) {
	if text := strings.TrimSpace(strings.Join(args, " ")); text != "" {
		return text, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, maxTask+1))
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	if len(b) > maxTask {
		return "", fmt.Errorf("the task on stdin is larger than %d MB", maxTask>>20)
	}
	return strings.TrimSpace(string(b)), nil
}

// found maps a yes-or-no to the exit contract.
func found(yes bool) int {
	if yes {
		return exitYes
	}
	return exitNo
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "lore:", err)
	return exitErr
}

// usage gives a flag set a synopsis line and takes the printing away from
// the flag package, which would put a parse error and the usage on the
// same stream. They belong on different ones, and usageCode chooses.
func usage(fs *flag.FlagSet, synopsis string) {
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: %s\n", synopsis)
		fs.PrintDefaults()
	}
}

// usageCode maps a flag parse error to an exit code. Help that was asked
// for is the output of a successful command, so it goes to stdout and
// `lore find -h | less` works; a misuse is a diagnostic, so it goes to
// stderr and leaves stdout empty for whatever was parsing it.
func usageCode(fs *flag.FlagSet, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		fs.SetOutput(os.Stdout)
		fs.Usage()
		return exitYes
	}
	fs.SetOutput(os.Stderr)
	fmt.Fprintln(os.Stderr, "lore:", err)
	fs.Usage()
	return exitErr
}

// usageErr reports a command called with the wrong arguments. Go's flag
// package stops at the first thing that is not a flag, so `lore new x -d
// dir` leaves -d sitting in the arguments — which is a confusing way to
// be told the argument count is wrong, and worth one line to explain.
func usageErr(fs *flag.FlagSet) int {
	fs.SetOutput(os.Stderr)
	for _, a := range fs.Args() {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(os.Stderr, "lore: %s must come before the arguments, not after\n", a)
			break
		}
	}
	fs.Usage()
	return exitErr
}
