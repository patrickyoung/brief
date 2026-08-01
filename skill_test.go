package main

import (
	"strings"
	"testing"
)

// TestSplitFindsTheFrontmatter covers the shapes a SKILL.md actually
// arrives in: written on a Mac, written on Windows, saved by an editor
// that adds a byte order mark, and closed with the other marker YAML
// allows.
func TestSplitFindsTheFrontmatter(t *testing.T) {
	for _, tc := range []struct {
		name, in, front, body string
		bodyAt                int
		wantErr               string
	}{
		{name: "plain", in: "---\nname: a\n---\nbody\n", front: "name: a", body: "body\n", bodyAt: 4},
		{name: "crlf", in: "---\r\nname: a\r\n---\r\nbody\r\n", front: "name: a\r", body: "body\r\n", bodyAt: 4},
		{name: "bom", in: "\ufeff---\nname: a\n---\nbody\n", front: "name: a", body: "body\n", bodyAt: 4},
		{name: "dots", in: "---\nname: a\n...\nbody\n", front: "name: a", body: "body\n", bodyAt: 4},
		{name: "empty body", in: "---\nname: a\n---\n", front: "name: a", body: "", bodyAt: 4},
		{name: "none", in: "# just markdown\n", wantErr: "must open with a ---"},
		{name: "unterminated", in: "---\nname: a\nbody\n", wantErr: "never closed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			front, _, body, bodyAt, err := split([]byte(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if front != tc.front || body != tc.body || bodyAt != tc.bodyAt {
				t.Fatalf("front %q body %q at %d", front, body, bodyAt)
			}
		})
	}
}

// TestParseNamesTheLine. Every diagnostic brief prints has to point at a
// line in the file a person is looking at, not at a line in the
// frontmatter counted separately.
func TestParseNamesTheLine(t *testing.T) {
	s, err := parseSkill("d", []byte("---\nname: a\ndescription: b\nlicense: MIT\n---\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]int{"name": 2, "description": 3, "license": 4} {
		if got := s.at(key); got != want {
			t.Errorf("%s is on line %d, want %d", key, got, want)
		}
	}
	if s.bodyAt != 6 {
		t.Errorf("body starts at %d, want 6", s.bodyAt)
	}
}

// TestParseKeepsWhatItDoesNotUnderstand. Real skills carry fields the
// specification never defined; refusing to read one would make brief
// useless against the skills that exist.
func TestParseKeepsWhatItDoesNotUnderstand(t *testing.T) {
	s, err := parseSkill("d", []byte("---\nname: a\ndescription: b\nreferences:\n  - one\n  - two\n---\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.name != "a" || s.desc != "b" {
		t.Fatalf("the fields it does understand were lost: %+v", s)
	}
	var keys []string
	for _, f := range s.fields {
		keys = append(keys, f.key)
	}
	if strings.Join(keys, ",") != "name,description,references" {
		t.Fatalf("fields %v", keys)
	}
}

// TestParseSeesADuplicate. YAML keeps the first of two identical keys and
// silently drops the second, which is how an author edits a description
// and watches nothing change.
func TestParseSeesADuplicate(t *testing.T) {
	s, err := parseSkill("d", []byte("---\nname: a\ndescription: first\ndescription: second\n---\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.desc != "first" {
		t.Errorf("kept %q, but every parser keeps the first", s.desc)
	}
	if len(s.dupes) != 1 || s.dupes[0].line != 4 {
		t.Errorf("duplicates %+v", s.dupes)
	}
}

// TestParseSurvivesBrokenYAML. A skill that will not parse still has to
// come back as something lint can talk about.
func TestParseSurvivesBrokenYAML(t *testing.T) {
	s, err := parseSkill("d", []byte("---\nname: [unclosed\n---\n\nbody\n"))
	if err == nil {
		t.Fatal("broken YAML parsed")
	}
	if s == nil {
		t.Fatal("nothing came back to report on")
	}
}

// TestInstructionsAreWhatBelongsInAPrompt: the body, without the
// frontmatter that was only ever there to choose the skill.
func TestInstructionsAreWhatBelongsInAPrompt(t *testing.T) {
	s, err := parseSkill("d", []byte("---\nname: a\ndescription: b\n---\n\n\n# Title\n\nDo the thing.\n\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.instructions(); got != "# Title\n\nDo the thing.\n" {
		t.Fatalf("%q", got)
	}
	empty, _ := parseSkill("d", []byte("---\nname: a\n---\n\n\n"))
	if empty.instructions() != "" {
		t.Fatalf("an empty body is empty, not whitespace: %q", empty.instructions())
	}
}

// TestValidNameIsTheSpecification. The name rule is also the security
// boundary: everything that resolves a reference goes through it first.
func TestValidNameIsTheSpecification(t *testing.T) {
	for _, ok := range []string{"pdf-processing", "a", "data-analysis", "s3", "a1-b2-c3"} {
		if !validName(ok) {
			t.Errorf("%q should be a legal name", ok)
		}
	}
	for _, bad := range []string{
		"", "PDF-Processing", "-pdf", "pdf-", "pdf--processing", "pdf processing",
		"pdf_processing", "../etc", "a/b", ".", "..", "pdf.md", strings.Repeat("a", 65),
	} {
		if validName(bad) {
			t.Errorf("%q should not be a legal name", bad)
		}
	}
}
