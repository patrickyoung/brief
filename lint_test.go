package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lintOne writes a SKILL.md into a directory of the given name and returns
// everything lint has to say about it.
func lintOne(t *testing.T, name, file string) []problem {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	return check(dir)
}

func says2(ps []problem, sub string) bool {
	for _, p := range ps {
		if strings.Contains(p.String(), sub) {
			return true
		}
	}
	return false
}

func onlyWarnings(ps []problem) bool {
	for _, p := range ps {
		if !p.warn {
			return false
		}
	}
	return true
}

// TestLintQuotesTheRuleItEnforced. Each clause of the specification gets
// its own message, because "invalid name" tells an author nothing they did
// not already suspect.
func TestLintQuotesTheRuleItEnforced(t *testing.T) {
	good := "Extracts tables from PDF files. Use when a task involves a PDF or a scan."
	for _, tc := range []struct{ name, dir, file, want string }{
		{"no name", "x",
			"---\ndescription: " + good + "\n---\n\nbody\n", "no name"},
		{"uppercase", "x",
			"---\nname: Skill\ndescription: " + good + "\n---\n\nbody\n", "uppercase"},
		{"leading hyphen", "x",
			"---\nname: -skill\ndescription: " + good + "\n---\n\nbody\n", "starts with a hyphen"},
		{"trailing hyphen", "x",
			"---\nname: skill-\ndescription: " + good + "\n---\n\nbody\n", "ends with a hyphen"},
		{"double hyphen", "x",
			"---\nname: a--b\ndescription: " + good + "\n---\n\nbody\n", "consecutive hyphens"},
		{"underscore", "x",
			"---\nname: a_b\ndescription: " + good + "\n---\n\nbody\n", "may only hold"},
		{"too long", "x",
			"---\nname: " + strings.Repeat("a", 65) + "\ndescription: " + good + "\n---\n\nbody\n", "65 characters (max 64)"},
		{"name is not the directory", "elsewhere",
			"---\nname: skill\ndescription: " + good + "\n---\n\nbody\n", "will not load"},
		{"no description", "skill",
			"---\nname: skill\n---\n\nbody\n", "no description"},
		{"empty description", "skill",
			"---\nname: skill\ndescription: \"\"\n---\n\nbody\n", "description is empty"},
		{"description too long", "skill",
			"---\nname: skill\ndescription: " + strings.Repeat("a", 1025) + "\n---\n\nbody\n", "(max 1024)"},
		{"description is a list", "skill",
			"---\nname: skill\ndescription:\n  - a\n  - b\n---\n\nbody\n", "must be a plain string"},
		{"compatibility too long", "skill",
			"---\nname: skill\ndescription: " + good + "\ncompatibility: " + strings.Repeat("a", 501) + "\n---\n\nbody\n", "(max 500)"},
		{"metadata is not a mapping", "skill",
			"---\nname: skill\ndescription: " + good + "\nmetadata: nope\n---\n\nbody\n", "metadata must be a mapping"},
		{"metadata value is not a string", "skill",
			"---\nname: skill\ndescription: " + good + "\nmetadata:\n  version: [1]\n---\n\nbody\n", "string to string"},
		{"allowed-tools is a list", "skill",
			"---\nname: skill\ndescription: " + good + "\nallowed-tools:\n  - Read\n---\n\nbody\n", "one string of space-separated"},
		{"duplicate key", "skill",
			"---\nname: skill\ndescription: " + good + "\ndescription: again\n---\n\nbody\n", "duplicate key"},
		{"no body", "skill",
			"---\nname: skill\ndescription: " + good + "\n---\n", "no instructions"},
		{"no frontmatter", "skill",
			"# just markdown\n", "must open with a ---"},
		{"unterminated frontmatter", "skill",
			"---\nname: skill\n", "never closed"},
		{"broken YAML", "skill",
			"---\nname: [unclosed\ndescription: " + good + "\n---\n\nbody\n", "not valid YAML"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := lintOne(t, tc.dir, tc.file)
			if !says2(ps, tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, ps)
			}
			if onlyWarnings(ps) {
				t.Fatalf("%q should be an error, not advice: %v", tc.want, ps)
			}
		})
	}
}

// TestLintAdvisesWithoutFailing. The specification's budgets and the shape
// of a good description are advice; a linter that fails a build over
// advice is a linter somebody turns off.
func TestLintAdvisesWithoutFailing(t *testing.T) {
	for _, tc := range []struct{ name, file, want string }{
		{"unknown field",
			"---\nname: skill\ndescription: Does the thing. Use when the thing needs doing here.\nauthor: nobody\n---\n\nbody\n",
			"unknown field"},
		{"description does not say when",
			"---\nname: skill\ndescription: A comprehensive toolkit for the processing of documents.\n---\n\nbody\n",
			"not when to use it"},
		{"description too short",
			"---\nname: skill\ndescription: Use it.\n---\n\nbody\n",
			"too short to choose by"},
		{"description is expensive",
			"---\nname: skill\ndescription: Use when " + strings.Repeat("a", 601) + "\n---\n\nbody\n",
			"every agent loads it at startup"},
		{"body is long",
			"---\nname: skill\ndescription: Does the thing. Use when the thing needs doing here.\n---\n\n" + strings.Repeat("line\n", 501),
			"asks for under 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps := lintOne(t, "skill", tc.file)
			if !says2(ps, tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, ps)
			}
			if !onlyWarnings(ps) {
				t.Fatalf("%q should be advice, not an error: %v", tc.want, ps)
			}
		})
	}
}

// TestLintAcceptsEveryOptionalField. The specification's optional fields
// are legal, and a linter that warns about them teaches authors to write
// less than the specification allows.
func TestLintAcceptsEveryOptionalField(t *testing.T) {
	ps := lintOne(t, "skill", `---
name: skill
description: Extracts tables from PDF files. Use when a task involves a PDF.
license: Apache-2.0
compatibility: Requires Python 3.14+ and uv
allowed-tools: Bash(git:*) Bash(jq:*) Read
metadata:
  author: example-org
  version: "1.0"
---

Do the thing.
`)
	if len(ps) != 0 {
		t.Fatalf("a fully specified skill should be clean: %v", ps)
	}
}

// TestLintFindsAReferenceThatIsNotThere. Progressive disclosure makes this
// failure invisible: the agent reads SKILL.md, decides it needs
// references/FORMS.md, and finds nothing. The skill lints clean everywhere
// else and is broken exactly when it is used.
func TestLintFindsAReferenceThatIsNotThere(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skill")
	os.MkdirAll(filepath.Join(dir, "references"), 0o755)
	os.WriteFile(filepath.Join(dir, "references", "THERE.md"), []byte("here\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: skill
description: Extracts tables from PDF files. Use when a task involves a PDF.
---

See [the guide](references/THERE.md), and also [the other one](references/GONE.md).

Run scripts/missing.py to start.

The website at https://example.com/nope.md is not our problem, nor is [an
anchor](#section).

`+"```"+`
scripts/an-example-nobody-has.py
`+"```"+`
`), 0o644)

	ps := check(dir)
	if !says2(ps, "references/GONE.md, which is not there") {
		t.Fatalf("missed the broken link: %v", ps)
	}
	if !says2(ps, "scripts/missing.py, which is not there") {
		t.Fatalf("missed the broken bare path: %v", ps)
	}
	if says2(ps, "THERE.md, which is not") {
		t.Fatalf("complained about a file that is there: %v", ps)
	}
	if says2(ps, "example.com") || says2(ps, "#section") {
		t.Fatalf("a URL or an anchor is not a file: %v", ps)
	}
	// A path inside a fence is an illustration. A linter that shouts at
	// illustrations is a linter people switch off.
	if says2(ps, "an-example-nobody-has") {
		t.Fatalf("complained about an example: %v", ps)
	}
}

// TestLintFindsAFileNothingMentions — the same rule from the other side. A
// bundled file no instruction names is not disclosed progressively, it is
// not disclosed at all.
func TestLintFindsAFileNothingMentions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skill")
	os.MkdirAll(filepath.Join(dir, "references"), 0o755)
	os.WriteFile(filepath.Join(dir, "references", "SEEN.md"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "references", "UNSEEN.md"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: skill
description: Extracts tables from PDF files. Use when a task involves a PDF.
---

Read [this](references/SEEN.md).
`), 0o644)

	ps := check(dir)
	if !says2(ps, "1 file(s) in references/ are never mentioned") {
		t.Fatalf("wanted the orphan: %v", ps)
	}
	if !onlyWarnings(ps) {
		t.Fatalf("this is advice, not an error: %v", ps)
	}
}

// TestLintReportsAMissingSkillOnTheDirectory. There is no line to point
// at, so the diagnostic names the directory instead of inventing one.
func TestLintReportsAMissingSkillOnTheDirectory(t *testing.T) {
	dir := t.TempDir()
	ps := check(dir)
	if len(ps) != 1 || !strings.Contains(ps[0].String(), "no SKILL.md") {
		t.Fatalf("%v", ps)
	}
	if strings.Contains(ps[0].String(), ":1:") {
		t.Fatalf("invented a line: %v", ps)
	}
}

// TestProblemsPrintLikeACompiler, because every editor and every pipeline
// already knows that shape.
func TestProblemsPrintLikeACompiler(t *testing.T) {
	if got := (problem{file: "a/SKILL.md", line: 3, msg: "bad"}).String(); got != "a/SKILL.md:3: bad" {
		t.Errorf("%q", got)
	}
	if got := (problem{file: "a/SKILL.md", line: 3, msg: "meh", warn: true}).String(); got != "a/SKILL.md:3: warning: meh" {
		t.Errorf("%q", got)
	}
	if got := (problem{file: "a", msg: "no SKILL.md"}).String(); got != "a: no SKILL.md" {
		t.Errorf("%q", got)
	}
}
