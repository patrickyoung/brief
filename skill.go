package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// maxSkill bounds a SKILL.md. The specification asks for under 500 lines
// and recommends moving detail into referenced files, so a megabyte is
// already an order of magnitude past the point where lint complains. The
// bound is an error, never a truncation: half a procedure that reads like
// a whole one is the one failure nothing downstream can detect.
const maxSkill = 1 << 20

// nameOK is the specification's name rule, whole: lowercase letters,
// digits and single interior hyphens. The four clauses it is written from
// — no leading hyphen, no trailing hyphen, no doubled hyphen, nothing
// outside a-z0-9 — are checked separately by lint, which owes an author a
// message that says which one they broke.
var nameOK = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validName reports whether a string could name a skill. Everything that
// resolves a reference goes through it first, which is what keeps a name
// from ever being a path: no dot, no slash, no leading dash, so ../ and
// /etc and -rf cannot arrive dressed as a skill.
func validName(s string) bool {
	return utf8.RuneCountInString(s) <= maxName && nameOK.MatchString(s)
}

// skill is one directory that follows the Agent Skills specification: a
// SKILL.md holding YAML frontmatter — what this is, and when to use it —
// followed by Markdown instructions.
//
// The fields are the specification's fields and nothing else. brief does
// not add any, because a field only brief understands is a field no agent
// will act on, and a skill that only works here is not a skill.
type skill struct {
	dir  string // the directory; its basename is the name skills resolve by
	body string // everything after the frontmatter

	name    string
	desc    string
	license string
	compat  string
	tools   string
	meta    map[string]string

	// The frontmatter as it was written: every key in order, with the line
	// it was on and the node it held. lint needs all three — a diagnostic
	// that cannot name a line makes the reader search, and a description
	// that was accidentally a YAML list is only catchable by kind.
	fields []field
	node   map[string]*yaml.Node
	dupes  []field
	bodyAt int // the file line the body starts on, counting from one
}

type field struct {
	key  string
	line int
}

// at returns the line a frontmatter key was written on, or 1 for a key
// that was never written at all — a missing required field is a fact about
// the top of the file.
func (s *skill) at(key string) int {
	for _, f := range s.fields {
		if f.key == key {
			return f.line
		}
	}
	return 1
}

// has reports whether a key appeared in the frontmatter, which is a
// different question from whether it holds anything: `description:` with
// nothing after it is present and empty, and the two need different
// messages.
func (s *skill) has(key string) bool {
	_, ok := s.node[key]
	return ok
}

// path returns the SKILL.md this skill was read from.
func (s *skill) path() string { return filepath.Join(s.dir, "SKILL.md") }

// readSkill reads and parses the SKILL.md in dir.
//
// It is deliberately forgiving: anything it can extract, it keeps, and a
// malformed field is left for lint to name. A reader that refused every
// imperfect skill would be useless against the skills that actually exist,
// and a linter that only ever sees perfect input has nothing to say.
func readSkill(dir string) (*skill, error) {
	p := filepath.Join(dir, "SKILL.md")
	fi, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: no SKILL.md", dir)
		}
		return nil, err
	}
	if fi.Size() > maxSkill {
		return nil, fmt.Errorf("%s is larger than %d MB", p, maxSkill>>20)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return parseSkill(dir, data)
}

func parseSkill(dir string, data []byte) (*skill, error) {
	s := &skill{dir: dir, node: map[string]*yaml.Node{}}
	front, frontAt, body, bodyAt, err := split(data)
	s.body, s.bodyAt = body, bodyAt
	if err != nil {
		return s, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		// The parser counts from the top of the frontmatter; the reader
		// counts from the top of the file. Only one of them is looking at
		// the file, so the message is rewritten to agree with them.
		return s, fmt.Errorf("frontmatter is not valid YAML: %s", shift(err.Error(), frontAt-1))
	}
	if len(doc.Content) == 0 {
		return s, errors.New("frontmatter is empty")
	}
	m := doc.Content[0]
	// Shift every line reported by the YAML parser to the line of the file
	// it came from, once, here. Every diagnostic downstream then names a
	// line a person can jump to without doing arithmetic.
	var shift func(*yaml.Node)
	shift = func(n *yaml.Node) {
		n.Line += frontAt - 1
		for _, c := range n.Content {
			shift(c)
		}
	}
	shift(m)
	if m.Kind != yaml.MappingNode {
		return s, errors.New("frontmatter must be a mapping of fields")
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		f := field{key: k.Value, line: k.Line}
		if _, seen := s.node[k.Value]; seen {
			s.dupes = append(s.dupes, f)
			continue // the first one is the one every parser keeps
		}
		s.fields = append(s.fields, f)
		s.node[k.Value] = v
		switch k.Value {
		case "name":
			v.Decode(&s.name)
		case "description":
			v.Decode(&s.desc)
		case "license":
			v.Decode(&s.license)
		case "compatibility":
			v.Decode(&s.compat)
		case "allowed-tools":
			v.Decode(&s.tools)
		case "metadata":
			v.Decode(&s.meta)
		}
	}
	return s, nil
}

// split separates YAML frontmatter from the Markdown body.
//
// The file opens with a --- line and the frontmatter ends at the next line
// that is --- or ... alone. Both line numbers come back counted from one,
// in the file's own numbering, because that is the only numbering a person
// reading the diagnostic has in front of them.
func split(data []byte) (front string, frontAt int, body string, bodyAt int, err error) {
	s := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || trim(lines[0]) != "---" {
		return "", 0, s, 1, errors.New("no YAML frontmatter: the file must open with a --- line")
	}
	for i := 1; i < len(lines); i++ {
		if t := trim(lines[i]); t == "---" || t == "..." {
			return strings.Join(lines[1:i], "\n"), 2,
				strings.Join(lines[i+1:], "\n"), i + 2, nil
		}
	}
	return "", 0, "", 0, errors.New("frontmatter is never closed: no second --- line")
}

func trim(s string) string { return strings.TrimRight(s, "\r") }

// yamlLine matches the way the YAML parser names a line in its errors.
var yamlLine = regexp.MustCompile(`line (\d+)`)

// shift moves every line number in a YAML diagnostic into the file's own
// numbering, so a message about line 2 of the frontmatter reads as line 3
// of the SKILL.md a person has open.
func shift(msg string, by int) string {
	return yamlLine.ReplaceAllStringFunc(msg, func(m string) string {
		n, err := strconv.Atoi(m[len("line "):])
		if err != nil {
			return m
		}
		return fmt.Sprintf("line %d", n+by)
	})
}

// instructions is the body as it should be handed to a model: the
// frontmatter stripped, because name and description are how an agent
// chose this skill and repeating them wastes the context the skill is
// about to need, and the blank lines around it removed, because a prompt
// composed of several pieces should not depend on where each one happened
// to stop.
func (s *skill) instructions() string {
	b := strings.Trim(s.body, "\n")
	if b == "" {
		return ""
	}
	return b + "\n"
}
