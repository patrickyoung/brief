package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAsk installs a stand-in for ask that records exactly how it was
// called and answers with reply. Every assertion about what brief sends a
// model is made against what this recorded, which is the only place the
// disclosure invariant can be checked from: the claim is about bytes that
// left the process, not about intentions in the source.
func fakeAsk(t *testing.T, reply string, code int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reply"), []byte(reply), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"" + dir + "/argv\"\n" +
		": > \"" + dir + "/system\"\n" +
		": > \"" + dir + "/attachment\"\n" +
		"next=\n" +
		"for arg do\n" +
		"  case $next in\n" +
		"    system) printf '%s' \"$arg\" > \"" + dir + "/system\"; next=; continue ;;\n" +
		"    attachment) cat \"$arg\" > \"" + dir + "/attachment\"; next=; continue ;;\n" +
		"  esac\n" +
		"  case $arg in -S) next=system ;; -a) next=attachment ;; esac\n" +
		"done\n" +
		"cat > \"" + dir + "/stdin\"\n" +
		"cat \"" + dir + "/reply\"\n" +
		"echo 'ask: something went wrong' >&2\n" +
		"exit " + itoa(code) + "\n"
	bin := filepath.Join(dir, "ask")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASK", bin)
	t.Setenv("BRIEF_DIR", filepath.Join(dir, "brief"))
	return dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func recorded(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("ask was never run: %v", err)
	}
	return string(b)
}

// secretBody is what a skill's instructions look like when the test needs
// to prove they did not travel.
const secretBody = "STEP ONE: the-body-must-not-travel-4c1f"

func catalogueTree(t *testing.T) string {
	t.Helper()
	return tree(t, map[string]string{
		"pdf-processing/SKILL.md": skillFile("pdf-processing",
			"Extract text and tables from PDFs. Use when a task involves a PDF.", secretBody),
		"pdf-processing/scripts/extract.py": "print('" + secretBody + "')\n",
		"web-perf/SKILL.md": skillFile("web-perf",
			"Measures Core Web Vitals. Use when a page is slow.", secretBody),
	})
}

// TestFindAskSendsLevelOneAndNothingElse is the disclosure invariant,
// asserted from the outside: the catalogue goes to the model, and no
// body, script or bundled file ever does. It is the reason choosing a
// skill costs a few hundred tokens instead of a context window, and the
// reason a private procedure stays on the machine that holds it.
func TestFindAskSendsLevelOneAndNothingElse(t *testing.T) {
	withPath(t, catalogueTree(t))
	dir := fakeAsk(t, "web-perf\n", 0)

	code, out, _ := exec(t, "", "find", "-ask", "-q", "my page is slow")
	if code != exitYes || out != "web-perf\n" {
		t.Fatalf("code %d, stdout %q", code, out)
	}

	sent := recorded(t, dir, "attachment") + recorded(t, dir, "stdin") + recorded(t, dir, "argv") + recorded(t, dir, "system")
	if strings.Contains(sent, secretBody) {
		t.Fatalf("a skill body reached the model:\n%s", sent)
	}
	for _, want := range []string{"pdf-processing\t", "web-perf\t", "Core Web Vitals", "my page is slow"} {
		if !strings.Contains(sent, want) {
			t.Fatalf("%q never reached the model:\n%s", want, sent)
		}
	}
	// The catalogue attachment is the same bytes ls prints. If those two ever
	// drift, `brief ls | ask ...` stops being the thing brief automates.
	_, listing, _ := exec(t, "", "ls")
	if recorded(t, dir, "attachment") != listing {
		t.Fatalf("what was sent is not what ls prints:\n%q\n%q", recorded(t, dir, "attachment"), listing)
	}
	if task := recorded(t, dir, "stdin"); task != "my page is slow" {
		t.Fatalf("task stdin=%q", task)
	}
}

// TestFindAskKeepsItsOwnConversation. Choosing a skill must not become a
// turn in the conversation the skill is about to be used for: ask
// continues by default, and a caller who ran brief would find their next
// question answered with a catalogue on the model's mind.
func TestFindAskKeepsItsOwnConversation(t *testing.T) {
	withPath(t, catalogueTree(t))
	dir := fakeAsk(t, "web-perf\n", 0)
	t.Setenv("BRIEF_MODEL", "anthropic/cheap-model")
	t.Setenv("BRIEF_EFFORT", "low")

	if code, _, _ := exec(t, "", "find", "-ask", "-q", "slow page"); code != exitYes {
		t.Fatalf("code %d", code)
	}
	argv := strings.Split(strings.TrimSpace(recorded(t, dir, "argv")), "\n")
	var sess string
	flags := map[string]bool{}
	for i, a := range argv {
		flags[a] = true
		if a == "-f" && i+1 < len(argv) {
			sess = argv[i+1]
		}
	}
	if !flags["-n"] {
		t.Error("ask was not told to start a new conversation (-n)")
	}
	if !flags["-m"] || !flags["anthropic/cheap-model"] {
		t.Error("BRIEF_MODEL did not reach ask")
	}
	if !flags["-effort"] || !flags["low"] {
		t.Error("BRIEF_EFFORT did not reach ask")
	}
	if !strings.HasPrefix(sess, filepath.Join(os.Getenv("BRIEF_DIR"), "find")) {
		t.Errorf("the session is not brief's own: %q", sess)
	}
	if !strings.HasSuffix(sess, ".jsonl") {
		t.Errorf("the session is not a session: %q", sess)
	}
	if strings.Contains(recorded(t, dir, "argv"), "slow page") {
		t.Errorf("sensitive task leaked through process argv: %v", argv)
	}
	if task := recorded(t, dir, "stdin"); task != "slow page" {
		t.Errorf("the task did not reach Ask as user text: %q", task)
	}
	if system := recorded(t, dir, "system"); strings.Contains(system, "slow page") || !strings.Contains(system, "choosing which skill") {
		t.Errorf("selector system prompt has wrong precedence/content: %q", system)
	}
}

// TestFindAskRefusesANameItNeverOffered. The answer is about to become a
// path. A model inventing a plausible skill name is an ordinary event; a
// model inventing one that brief then opens is a vulnerability.
func TestFindAskRefusesANameItNeverOffered(t *testing.T) {
	withPath(t, catalogueTree(t))
	for _, reply := range []string{"../../etc/passwd\n", "pdf-processor\n", "/etc/passwd\n"} {
		fakeAsk(t, reply, 0)
		code, out, errs := exec(t, "", "find", "-ask", "-q", "anything")
		if code != exitErr || out != "" {
			t.Fatalf("%q: code %d, stdout %q", reply, code, out)
		}
		if !strings.Contains(errs, "not in the catalogue") {
			t.Fatalf("%q: stderr %q", reply, errs)
		}
	}
}

// TestFindAskNoneIsNoNotAGuess. "Nothing fits" has to survive the trip
// back as an exit code, or a supervisor loop turns it into whichever skill
// sorted first.
func TestFindAskNoneIsNoNotAGuess(t *testing.T) {
	withPath(t, catalogueTree(t))
	fakeAsk(t, "none\n", 0)
	code, out, _ := exec(t, "", "find", "-ask", "-q", "bake bread")
	if code != exitNo || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
}

// TestFindAskCapsAtN. -n is a promise to the caller about how many lines
// they are about to read, so it is enforced here rather than trusted to
// the model.
func TestFindAskCapsAtN(t *testing.T) {
	withPath(t, catalogueTree(t))
	fakeAsk(t, "web-perf\npdf-processing\nweb-perf\n", 0)
	_, out, _ := exec(t, "", "find", "-ask", "-q", "-n", "2", "anything")
	if out != "web-perf\npdf-processing\n" {
		t.Fatalf("stdout %q", out)
	}
	fakeAsk(t, "web-perf\npdf-processing\n", 0)
	_, out, _ = exec(t, "", "find", "-ask", "-q", "anything")
	if out != "web-perf\n" {
		t.Fatalf("default is not one: %q", out)
	}
}

// TestFindAskReportsAskFailing. A model that could not be reached is not
// the same answer as no skill fitting, and a script has to be able to tell
// them apart.
func TestFindAskReportsAskFailing(t *testing.T) {
	withPath(t, catalogueTree(t))
	fakeAsk(t, "", 1)
	code, out, errs := exec(t, "", "find", "-ask", "-q", "anything")
	if code != exitErr || out != "" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	if !strings.Contains(errs, "something went wrong") {
		t.Fatalf("ask's own diagnosis was swallowed: %q", errs)
	}
	t.Setenv("ASK", filepath.Join(t.TempDir(), "no-such-ask"))
	if code, _, errs := exec(t, "", "find", "-ask", "-q", "anything"); code != exitErr || !strings.Contains(errs, "not installed") {
		t.Fatalf("missing ask: code %d, stderr %q", code, errs)
	}
}

// TestFindAskSaysWhereTheChoiceIsRecorded. The audit trail is only useful
// if the run says where it is.
func TestFindAskSaysWhereTheChoiceIsRecorded(t *testing.T) {
	withPath(t, catalogueTree(t))
	fakeAsk(t, "web-perf\n", 0)
	_, _, errs := exec(t, "", "find", "-ask", "slow page")
	if !strings.Contains(errs, "ask replay -check") || !strings.Contains(errs, ".jsonl") {
		t.Fatalf("stderr %q", errs)
	}
	if _, _, errs := exec(t, "", "find", "-ask", "-q", "slow page"); errs != "" {
		t.Fatalf("-q left something on stderr: %q", errs)
	}
}

// --- the ranking that needs no model ------------------------------------

func entries(pairs ...string) []entry {
	var out []entry
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, entry{name: pairs[i], desc: pairs[i+1]})
	}
	return out
}

// TestRankWeighsWordsByHowRareTheyAre. The word that appears in one
// description decides; the word that appears in all of them cannot.
func TestRankWeighsWordsByHowRareTheyAre(t *testing.T) {
	cat := entries(
		"pdf-processing", "Extract tables from PDF files. Use when a task involves documents.",
		"web-perf", "Measure page load. Use when a task involves documents.",
		"wrangler", "Deploy workers. Use when a task involves documents.",
		"email", "Send mail. Use when a task involves documents.",
	)
	hits := rank(cat, "pull the tables out of this pdf")
	if len(hits) == 0 || hits[0].name != "pdf-processing" {
		t.Fatalf("ranking: %+v", hits)
	}
	// "documents" is in every description, so a task made only of it is not
	// evidence about anything and must not produce a winner.
	if hits := rank(cat, "documents"); len(hits) != 0 {
		t.Fatalf("a word every skill uses picked one: %+v", hits)
	}
}

// TestRankSaysNothingRatherThanGuess. A confidently wrong name is the one
// failure the next program in the pipe cannot detect.
func TestRankSaysNothingRatherThanGuess(t *testing.T) {
	cat := entries(
		"pdf-processing", "Extract tables from PDF files. Use when a task involves a PDF.",
		"web-perf", "Measure Core Web Vitals. Use when a page is slow.",
	)
	for _, task := range []string{"bake a sourdough loaf", "when should I use this", "", "   "} {
		if hits := rank(cat, task); len(hits) != 0 {
			t.Errorf("%q matched %s", task, hits[0].name)
		}
	}
}

// TestRankIsTheSameEveryRun. A filter whose answer depends on map
// iteration order is a filter nobody can build on.
func TestRankIsTheSameEveryRun(t *testing.T) {
	cat := entries(
		"alpha", "Handles widgets. Use when there are widgets.",
		"beta", "Handles widgets. Use when there are widgets.",
		"gamma", "Handles widgets. Use when there are widgets.",
	)
	for i := 0; i < 20; i++ {
		hits := rank(cat, "widgets")
		if len(hits) != 3 || hits[0].name != "alpha" {
			t.Fatalf("run %d: %+v", i, hits)
		}
	}
}

// TestRankStemsBothSides. A description writes "charting" and a task
// writes "chart". They are one word, and the ranking has to see that: the
// alternative here is a tie broken alphabetically, which is a confidently
// wrong answer wearing a plausible name.
func TestRankStemsBothSides(t *testing.T) {
	cat := entries(
		"xlsx", "Spreadsheets: formulas, formatting, charting, cleaning messy data.",
		"docx", "Word documents. Mentions a spreadsheet once, in passing.",
	)
	hits := rank(cat, "turn a spreadsheet into a chart")
	if len(hits) == 0 || hits[0].name != "xlsx" {
		t.Fatalf("ranking: %+v", hits)
	}
	for word, root := range map[string]string{
		"charting": "chart", "tables": "table", "processing": "process",
		"pdf": "pdf", "css": "css", "ring": "ring", "class": "class",
	} {
		if got := stem(word); got != root {
			t.Errorf("stem(%q) = %q, want %q", word, got, root)
		}
	}
}

// TestRankNeedsMoreThanACoincidence. A rare word scores highly precisely
// because it is rare, so one incidental match beats no match at all unless
// something stops it: "an opened box" finds a deck skill that says
// "opened" about a file. A single word counts only when it is most of the
// question.
func TestRankNeedsMoreThanACoincidence(t *testing.T) {
	cat := entries(
		"pptx", "PowerPoint decks. Any task where a .pptx is opened, edited or created.",
		"docx", "Word documents and templates.",
		"pdf", "PDF files: read them, fill forms, merge them.",
		"xlsx", "Spreadsheets, formulas and csv data.",
	)
	if hits := rank(cat, "customer wants a refund on an opened box"); len(hits) != 0 {
		t.Fatalf("one incidental word out of five picked %s", hits[0].name)
	}
	// One word that is the whole question is still a match.
	if hits := rank(cat, "pdf"); len(hits) == 0 || hits[0].name != "pdf" {
		t.Fatalf("a one-word task: %+v", hits)
	}
	// So is a skill the task named outright.
	if hits := rank(cat, "use the xlsx skill on this"); len(hits) == 0 || hits[0].name != "xlsx" {
		t.Fatalf("a name in the task: %+v", hits)
	}
}

// TestRankHearsAName. "run the web-perf skill" is not a guess about what
// was meant, and must not be outvoted by common words elsewhere.
func TestRankHearsAName(t *testing.T) {
	cat := entries(
		"web-perf", "Measures things.",
		"pdf-processing", "Extract tables from PDF files, a task about tables and files and a page.",
	)
	hits := rank(cat, "use the web-perf skill on this page of tables")
	if len(hits) == 0 || hits[0].name != "web-perf" {
		t.Fatalf("ranking: %+v", hits)
	}
}
