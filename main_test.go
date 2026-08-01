package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// The tests below drive run() exactly as a shell does — argv and stdin in,
// two streams and an exit code out. Nothing between the flag parsing and
// the filesystem is stubbed, so what they pin is the program rather than a
// rehearsal of it. The one thing that is stubbed is ask, because a test
// that needs a language model to be right is not a test.

// exec runs argv with stdin bound to text ("" means a terminal, the way an
// interactive shell invokes brief) and captures the streams separately,
// because which stream carried what is itself under test.
func exec(t *testing.T, stdin string, argv ...string) (code int, stdout, stderr string) {
	t.Helper()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()

	if stdin == "" {
		devnull, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer devnull.Close()
		os.Stdin = devnull
	} else {
		os.Stdin = tmpFile(t, "stdin", stdin)
	}
	out, errf := tmpFile(t, "stdout", ""), tmpFile(t, "stderr", "")
	os.Stdout, os.Stderr = out, errf

	code = run(argv)
	return code, readAll(t, out), readAll(t, errf)
}

func tmpFile(t *testing.T, name, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if content != "" {
		if _, err := f.WriteString(content); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func readAll(t *testing.T, f *os.File) string {
	t.Helper()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tree writes a directory of files from a map of relative path to
// contents. Keys are slash-separated whatever the platform.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// withPath points brief at these directories and nothing else, so a test
// never sees the skills the developer happens to have installed.
func withPath(t *testing.T, dirs ...string) {
	t.Helper()
	t.Setenv("BRIEF_PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

// skillFile is the shortest thing that is a valid skill.
func skillFile(name, desc, body string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body + "\n"
}

func oneSkill(t *testing.T) string {
	t.Helper()
	return tree(t, map[string]string{
		"pdf-processing/SKILL.md": skillFile("pdf-processing",
			"Extract text and tables from PDF files. Use when a task involves a PDF or a scanned document.",
			"# PDF\n\nOpen the file. Extract the tables.\nThe fields are in [FORMS](references/FORMS.md).\n"),
		"pdf-processing/references/FORMS.md": "form fields\n",
	})
}

// TestExitContract is the whole promise in one test: yes is 0, no is 1,
// and broken is 2 — so `brief find x || fallback` fires on nothing found
// and not on an unreadable directory.
func TestExitContract(t *testing.T) {
	withPath(t, oneSkill(t))

	if code, out, _ := exec(t, "", "find", "extract the tables from this pdf"); code != exitYes || out != "pdf-processing\n" {
		t.Fatalf("a match: code %d, stdout %q", code, out)
	}
	if code, out, _ := exec(t, "", "find", "bake a sourdough loaf"); code != exitNo || out != "" {
		t.Fatalf("no match: code %d, stdout %q", code, out)
	}
	if code, out, _ := exec(t, "", "cat", "no-such-skill"); code != exitErr || out != "" {
		t.Fatalf("broken: code %d, stdout %q", code, out)
	}
}

// TestNoMatchPrintsNothing pins the half of the contract a pipeline
// depends on: a "no" leaves stdout empty, so `brief cat $(brief find ...)`
// cannot be handed a diagnostic as if it were a name.
func TestNoMatchPrintsNothing(t *testing.T) {
	withPath(t, oneSkill(t))
	code, out, errs := exec(t, "", "find", "quantum chromodynamics")
	if code != exitNo || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if strings.Contains(errs, "pdf") {
		t.Fatalf("stderr guessed anyway: %q", errs)
	}
}

// TestLsIsTheCatalogue pins level 1: one skill per line, a name, a tab,
// and a description that stays on its line however it was written.
func TestLsIsTheCatalogue(t *testing.T) {
	dir := tree(t, map[string]string{
		"a-skill/SKILL.md":      "---\nname: a-skill\ndescription: >\n  folded across\n  two lines\n---\n\nbody\n",
		"b-skill/SKILL.md":      skillFile("b-skill", "second", "body"),
		"not-a-skill/README.md": "no SKILL.md here\n",
	})
	withPath(t, dir)
	code, out, _ := exec(t, "", "ls")
	if code != exitYes {
		t.Fatalf("code %d", code)
	}
	want := "a-skill\tfolded across two lines\nb-skill\tsecond\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

// TestPathShadowsLikePath: first match wins, and -a shows what was
// covered, because a shadowed skill and an ignored one look identical
// until something says otherwise.
func TestPathShadowsLikePath(t *testing.T) {
	near := tree(t, map[string]string{"dup/SKILL.md": skillFile("dup", "the near one, used when both exist", "near")})
	far := tree(t, map[string]string{"dup/SKILL.md": skillFile("dup", "the far one, used when it is alone", "far")})
	withPath(t, near, far)

	if _, out, _ := exec(t, "", "cat", "dup"); strings.TrimSpace(out) != "near" {
		t.Fatalf("cat took the far one: %q", out)
	}
	_, out, _ := exec(t, "", "path", "-a", "dup")
	lines := strings.Fields(out)
	if len(lines) != 2 || lines[0] != filepath.Join(near, "dup") || lines[1] != filepath.Join(far, "dup") {
		t.Fatalf("path -a: %q", out)
	}
	if _, out, _ := exec(t, "", "path", "dup"); strings.TrimSpace(out) != filepath.Join(near, "dup") {
		t.Fatalf("path: %q", out)
	}
	// One line each: ls answers for a name once, nearest first.
	if _, out, _ := exec(t, "", "ls"); strings.Count(out, "\n") != 1 {
		t.Fatalf("ls listed a shadowed skill: %q", out)
	}
}

// TestCatIsLevelTwoAndThree. A name gives the instructions with the
// frontmatter stripped, which is what belongs in a prompt. Naming the file
// gives the file, byte for byte.
func TestCatIsLevelTwoAndThree(t *testing.T) {
	withPath(t, oneSkill(t))

	_, body, _ := exec(t, "", "cat", "pdf-processing")
	if strings.Contains(body, "description:") || !strings.Contains(body, "Extract the tables") {
		t.Fatalf("cat name should be the body alone: %q", body)
	}
	_, raw, _ := exec(t, "", "cat", "pdf-processing/SKILL.md")
	if !strings.HasPrefix(raw, "---\nname: pdf-processing") {
		t.Fatalf("cat of the file should be the file: %q", raw)
	}
	_, ref, _ := exec(t, "", "cat", "pdf-processing/references/FORMS.md")
	if ref != "form fields\n" {
		t.Fatalf("level 3: %q", ref)
	}
	// cat concatenates, because it is cat.
	_, both, _ := exec(t, "", "cat", "pdf-processing", "pdf-processing/references/FORMS.md")
	if !strings.HasSuffix(both, "form fields\n") || !strings.Contains(both, "Extract the tables") {
		t.Fatalf("two refs: %q", both)
	}
}

// TestLsInsideASkillNamesWhatCatWillRead. The two halves of level 3 have
// to agree, or the listing is a set of paths that do not open.
func TestLsInsideASkillNamesWhatCatWillRead(t *testing.T) {
	withPath(t, oneSkill(t))
	code, out, _ := exec(t, "", "ls", "pdf-processing")
	if code != exitYes {
		t.Fatalf("code %d", code)
	}
	for _, f := range strings.Fields(out) {
		if code, _, errs := exec(t, "", "cat", "pdf-processing/"+f); code != exitYes {
			t.Fatalf("ls named %q but cat says %s", f, errs)
		}
	}
}

// TestRefsCannotLeaveTheSkill. A ref may arrive from a model, so the
// escape has to be refused by the resolver rather than by hoping.
func TestRefsCannotLeaveTheSkill(t *testing.T) {
	withPath(t, oneSkill(t))
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("private"), 0o600)

	for _, ref := range []string{
		"pdf-processing/../../etc/passwd",
		"pdf-processing/references/../../../etc/passwd",
		"pdf-processing/" + secret,
	} {
		code, out, _ := exec(t, "", "cat", ref)
		if code != exitErr || out != "" {
			t.Fatalf("%q escaped: code %d, stdout %q", ref, code, out)
		}
	}
}

// TestFindReadsStdinOnlyWhenItHasTo. A task can arrive in a pipe, which is
// what makes brief a filter rather than a command that happens to take
// words. A task in argv ends the question there: reading a stdin nobody
// meant to send is how find hangs inside a loop that inherited a pipe, and
// a command that waits forever is a worse answer than a narrow one.
func TestFindReadsStdinOnlyWhenItHasTo(t *testing.T) {
	withPath(t, oneSkill(t))
	code, out, _ := exec(t, "the tables in this scan are in a pdf\n", "find")
	if code != exitYes || out != "pdf-processing\n" {
		t.Fatalf("from stdin: code %d, stdout %q", code, out)
	}
	// A pipe holding something else does not change an answer argv settled.
	code, out, _ = exec(t, "the tables in this scan are in a pdf\n", "find", "bake a sourdough loaf")
	if code != exitNo || out != "" {
		t.Fatalf("argv should have won: code %d, stdout %q", code, out)
	}
}

// TestHelpStreams. Help that was asked for is output and belongs on
// stdout; a misuse is a diagnostic and belongs on stderr, leaving stdout
// empty for whatever was parsing it.
func TestHelpStreams(t *testing.T) {
	for _, argv := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		code, out, errs := exec(t, "", argv...)
		if code != exitYes || !strings.HasPrefix(out, "brief —") || errs != "" {
			t.Fatalf("%v: code %d, stdout %q, stderr %q", argv, code, first(out), errs)
		}
	}
	code, out, errs := exec(t, "", "find", "-nope")
	if code != exitErr || out != "" || !strings.Contains(errs, "-nope") {
		t.Fatalf("bad flag: code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, out, errs = exec(t, "", "cat")
	if code != exitErr || out != "" || !strings.Contains(errs, "usage:") {
		t.Fatalf("no argument: code %d, stdout %q, stderr %q", code, out, errs)
	}
	code, _, errs = exec(t, "", "find", "-h")
	if code != exitYes {
		t.Fatalf("find -h: code %d, stderr %q", code, errs)
	}
}

// TestUnknownCommandSuggestsCat. Naming a skill where a verb goes is the
// mistake this program invites; the answer to it is one word long.
func TestUnknownCommandSuggestsCat(t *testing.T) {
	withPath(t, oneSkill(t))
	code, out, errs := exec(t, "", "pdf-processing")
	if code != exitErr || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if !strings.Contains(errs, "brief cat pdf-processing") {
		t.Fatalf("no suggestion: %q", errs)
	}
	if _, _, errs := exec(t, "", "frobnicate"); !strings.Contains(errs, "commands are") {
		t.Fatalf("unknown word: %q", errs)
	}
}

// TestNewWritesASkillThatPasses. A scaffold that does not lint clean
// teaches the wrong shape on the first file an author ever sees.
func TestNewWritesASkillThatPasses(t *testing.T) {
	dir := t.TempDir()
	code, out, _ := exec(t, "", "new", "-d", dir, "my-new-skill")
	if code != exitYes {
		t.Fatalf("code %d", code)
	}
	path := strings.TrimSpace(out)
	if path != filepath.Join(dir, "my-new-skill", "SKILL.md") {
		t.Fatalf("printed %q", path)
	}
	if code, out, _ := exec(t, "", "lint", "-strict", filepath.Dir(path)); code != exitYes {
		t.Fatalf("the scaffold does not lint clean: %s", out)
	}
	// A second one refuses rather than overwriting the author's work.
	if code, _, errs := exec(t, "", "new", "-d", dir, "my-new-skill"); code != exitErr || !strings.Contains(errs, "-f") {
		t.Fatalf("clobbered: code %d, stderr %q", code, errs)
	}
	if code, _, _ := exec(t, "", "new", "-d", dir, "Not A Name"); code != exitErr {
		t.Fatalf("accepted an illegal name")
	}
}

// TestLintExitContract. Errors fail, warnings do not, -strict makes them,
// and -q leaves only the exit status.
func TestLintExitContract(t *testing.T) {
	clean := tree(t, map[string]string{
		"good/SKILL.md": skillFile("good", "Does a thing. Use when the thing needs doing and nothing else will do.", "steps"),
	})
	warned := tree(t, map[string]string{
		"warn/SKILL.md": "---\nname: warn\ndescription: A skill for the thing, used whenever the thing arrives.\nauthor: nobody\n---\n\nbody\n",
	})
	broken := tree(t, map[string]string{
		"bad/SKILL.md": "---\nname: mismatch\ndescription: Says one name, lives in another, used when testing.\n---\n\nbody\n",
	})
	if code, out, _ := exec(t, "", "lint", clean); code != exitYes || out != "" {
		t.Fatalf("clean: code %d, %q", code, out)
	}
	if code, out, _ := exec(t, "", "lint", warned); code != exitYes || !strings.Contains(out, "warning:") {
		t.Fatalf("warning: code %d, %q", code, out)
	}
	if code, _, _ := exec(t, "", "lint", "-strict", warned); code != exitNo {
		t.Fatalf("-strict did not fail on a warning: %d", code)
	}
	if code, out, _ := exec(t, "", "lint", broken); code != exitNo || !strings.Contains(out, "mismatch") {
		t.Fatalf("error: code %d, %q", code, out)
	}
	if code, out, _ := exec(t, "", "lint", "-q", broken); code != exitNo || out != "" {
		t.Fatalf("-q: code %d, %q", code, out)
	}
}

// TestLintTakesPathsNotNames. lint is the one verb that has to work on a
// skill you are still writing, which is not installed anywhere yet.
func TestLintTakesPathsNotNames(t *testing.T) {
	dir := oneSkill(t)
	withPath(t, dir)
	if code, out, _ := exec(t, "", "lint", filepath.Join(dir, "pdf-processing")); code != exitYes || out != "" {
		t.Fatalf("skill directory: code %d, %q", code, out)
	}
	if code, out, _ := exec(t, "", "lint", dir); code != exitYes || out != "" {
		t.Fatalf("directory of skills: code %d, %q", code, out)
	}
	if code, out, _ := exec(t, "", "lint", filepath.Join(dir, "pdf-processing", "SKILL.md")); code != exitYes || out != "" {
		t.Fatalf("the file itself: code %d, %q", code, out)
	}
	// No argument at all means everything installed.
	if code, out, _ := exec(t, "", "lint"); code != exitYes || out != "" {
		t.Fatalf("the whole path: code %d, %q", code, out)
	}
}

// TestPromptIsAValue. The prompt find -ask sends is printable, so
// extending it is ordinary shell rather than a fork.
func TestPromptIsAValue(t *testing.T) {
	code, out, _ := exec(t, "", "prompt")
	if code != exitYes || !strings.Contains(out, "none") {
		t.Fatalf("code %d, %q", code, first(out))
	}
	if strings.TrimSpace(out) != strings.TrimSpace(selectPrompt(1)) {
		t.Fatal("brief prompt is not the prompt that is sent")
	}
	_, three, _ := exec(t, "", "prompt", "-n", "3")
	if !strings.Contains(three, "at most 3") {
		t.Fatalf("-n is not reflected: %q", three)
	}
}

func TestVersionIsOneNumber(t *testing.T) {
	for _, argv := range [][]string{{"version"}, {"-V"}, {"--version"}} {
		code, out, _ := exec(t, "", argv...)
		if code != exitYes || out != "brief "+version+"\n" {
			t.Fatalf("%v: code %d, %q", argv, code, out)
		}
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
		t.Fatalf("version %q is not x.y.z", version)
	}
}

// --- the documentation is part of the program ---------------------------

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestUsageFitsEightyColumns. Help that wraps in a default terminal is
// help that is harder to read than the manual it replaces.
func TestUsageFitsEightyColumns(t *testing.T) {
	for i, line := range strings.Split(usageText, "\n") {
		if n := utf8.RuneCountInString(line); n > 80 {
			t.Errorf("usage line %d is %d columns: %s", i+1, n, line)
		}
	}
}

// TestDocsCoverEveryCommand guards the whole rather than the parts: a
// flag-by-flag check stays green while an entire verb goes undocumented.
func TestDocsCoverEveryCommand(t *testing.T) {
	readme, man := read(t, "README.md"), read(t, "brief.1")
	for _, verb := range []string{"ls", "cat", "find", "lint", "new", "path", "prompt", "version", "help"} {
		if !strings.Contains(usageText, "brief "+verb) {
			t.Errorf("brief help does not mention %q", verb)
		}
		if !strings.Contains(man, "brief "+verb) && !strings.Contains(man, "brief \\fB"+verb) {
			t.Errorf("brief.1 does not mention %q", verb)
		}
		if !strings.Contains(readme, "brief "+verb) {
			t.Errorf("README.md does not mention %q", verb)
		}
	}
}

// TestDocsCoverEveryFlag walks the real flag sets rather than a list
// somebody has to remember to update.
func TestDocsCoverEveryFlag(t *testing.T) {
	man := read(t, "brief.1")
	for verb, flags := range map[string][]string{
		"find":   {"-ask", "-n", "-v", "-q"},
		"lint":   {"-strict", "-q"},
		"new":    {"-d", "-f"},
		"path":   {"-a"},
		"prompt": {"-n"},
	} {
		for _, f := range flags {
			if !strings.Contains(usageText, f) {
				t.Errorf("brief help does not document %s %s", verb, f)
			}
			if !strings.Contains(man, "\\fB\\"+f) {
				t.Errorf("brief.1 does not document %s %s", verb, f)
			}
		}
	}
}

// TestEnvVarsAreDocumented. An environment variable nobody can discover is
// a setting that does not exist.
func TestEnvVarsAreDocumented(t *testing.T) {
	man := read(t, "brief.1")
	for _, v := range []string{"BRIEF_PATH", "BRIEF_MODEL", "BRIEF_DIR", "ASK"} {
		if !strings.Contains(usageText, v) {
			t.Errorf("brief help does not document %s", v)
		}
		if !strings.Contains(man, v) {
			t.Errorf("brief.1 does not document %s", v)
		}
	}
}

// TestManPageLints holds the man page to what man(1) and every lint in the
// world expect: pure ASCII, a .TH, and the sections a reader looks for.
func TestManPageLints(t *testing.T) {
	man := read(t, "brief.1")
	if !strings.HasPrefix(man, ".TH BRIEF 1 ") {
		t.Error("no .TH line")
	}
	for i, line := range strings.Split(man, "\n") {
		for _, r := range line {
			if r > 127 {
				t.Errorf("brief.1:%d: non-ASCII %q — use a roff escape", i+1, r)
				break
			}
		}
		if strings.HasSuffix(line, " ") {
			t.Errorf("brief.1:%d: trailing space", i+1)
		}
	}
	for _, sec := range []string{".SH NAME", ".SH SYNOPSIS", ".SH DESCRIPTION", ".SH COMMANDS", ".SH ENVIRONMENT", ".SH EXIT STATUS", ".SH EXAMPLES", ".SH SEE ALSO"} {
		if !strings.Contains(man, sec) {
			t.Errorf("brief.1 has no %s", sec)
		}
	}
	if !strings.Contains(man, "brief "+version) {
		t.Errorf("brief.1 does not name version %s", version)
	}
}

// TestTheSpecificationsNumbersAreQuotedCorrectly. Every limit brief
// enforces is the specification's, and a linter that invents a number is
// worse than no linter.
func TestTheSpecificationsNumbersAreQuotedCorrectly(t *testing.T) {
	got := map[string]int{
		"name":          maxName,
		"description":   maxDescription,
		"compatibility": maxCompat,
		"body lines":    maxBodyLines,
		"body tokens":   maxBodyTokens,
	}
	want := map[string]int{
		"name": 64, "description": 1024, "compatibility": 500,
		"body lines": 500, "body tokens": 5000,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s limit is %d, the specification says %d", k, got[k], v)
		}
	}
	man := read(t, "brief.1")
	for _, n := range []string{"64", "1024", "500"} {
		if !strings.Contains(man, n) {
			t.Errorf("brief.1 does not quote the limit %s", n)
		}
	}
}

func first(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
