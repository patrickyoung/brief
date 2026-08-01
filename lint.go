package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// The specification's own numbers, in one place, so a message can quote
// the rule it enforced rather than a bare integer.
const (
	maxName        = 64
	maxDescription = 1024
	maxCompat      = 500
	maxBodyLines   = 500 // "Keep your main SKILL.md under 500 lines"
	maxBodyTokens  = 5000
	fatDescription = 600 // where a level-1 line stops being cheap
)

// problem is one finding. It prints the way a compiler prints, because
// every editor and every pipeline in the world already knows that shape.
type problem struct {
	file string
	line int
	msg  string
	warn bool
}

func (p problem) String() string {
	where := p.file
	if p.line > 0 {
		where = fmt.Sprintf("%s:%d", p.file, p.line)
	}
	if p.warn {
		return where + ": warning: " + p.msg
	}
	return where + ": " + p.msg
}

// check reads a skill directory and returns everything the specification
// says about it, worst first is not the order — file order is, because the
// list is meant to be worked through from the top of the file down.
//
// Errors are violations: a rule in the specification says MUST, and this
// skill does not. Warnings are the specification's advice — the budgets,
// the shape of a good description — and its silences: a bundled file no
// instruction ever mentions is a file no agent will ever open.
func check(dir string) []problem {
	var ps []problem
	add := func(line int, warn bool, format string, a ...any) {
		ps = append(ps, problem{
			file: filepath.Join(dir, "SKILL.md"), line: line,
			msg: fmt.Sprintf(format, a...), warn: warn,
		})
	}

	s, err := readSkill(dir)
	if err != nil {
		if s == nil { // no SKILL.md at all, or unreadable
			return []problem{{file: dir, msg: strings.TrimPrefix(err.Error(), dir+": ")}}
		}
		add(1, false, "%s", err)
		return ps
	}

	for _, d := range s.dupes {
		add(d.line, false, "duplicate key %q; every parser keeps the first and drops this one", d.key)
	}
	checkName(s, dir, add)
	checkDescription(s, add)
	checkOptional(s, add)
	for _, f := range s.fields {
		switch f.key {
		case "name", "description", "license", "compatibility", "metadata", "allowed-tools":
		default:
			add(f.line, true, "unknown field %q; the specification defines name, description, license, compatibility, metadata and allowed-tools", f.key)
		}
	}
	checkBody(s, dir, add)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].line < ps[j].line })
	return ps
}

type addFunc func(line int, warn bool, format string, a ...any)

// checkName enforces the name rules, which exist because the name is not a
// label: it is how an agent finds the directory. The specification's four
// clauses each get their own message, since "invalid name" tells an author
// nothing they did not already suspect.
func checkName(s *skill, dir string, add addFunc) {
	line := s.at("name")
	if !s.has("name") {
		add(1, false, "no name: the frontmatter must have one")
		return
	}
	if !scalar(s.node["name"]) {
		add(line, false, "name must be a plain string")
		return
	}
	n := s.name
	switch {
	case n == "":
		add(line, false, "name is empty")
		return
	case utf8.RuneCountInString(n) > maxName:
		add(line, false, "name is %d characters (max %d)", utf8.RuneCountInString(n), maxName)
	}
	switch {
	case strings.HasPrefix(n, "-"):
		add(line, false, "name %q starts with a hyphen", n)
	case strings.HasSuffix(n, "-"):
		add(line, false, "name %q ends with a hyphen", n)
	case strings.Contains(n, "--"):
		add(line, false, "name %q has consecutive hyphens", n)
	case n != strings.ToLower(n):
		add(line, false, "name %q has uppercase letters; names are lowercase", n)
	case !nameOK.MatchString(n):
		add(line, false, "name %q may only hold a-z, 0-9 and single hyphens", n)
	}
	// The one rule that is about two places at once, and the one that
	// breaks a skill most quietly: the agent resolves the directory, reads
	// the name, finds they disagree, and skips a skill that looks installed.
	if base := filepath.Base(mustAbs(dir)); nameOK.MatchString(n) && n != base {
		add(line, false, "name %q is not the directory name %q; they must match or the skill will not load", n, base)
	}
}

func checkDescription(s *skill, add addFunc) {
	line := s.at("description")
	if !s.has("description") {
		add(1, false, "no description: it is the only thing an agent reads before choosing this skill")
		return
	}
	if !scalar(s.node["description"]) {
		add(line, false, "description must be a plain string")
		return
	}
	d := strings.TrimSpace(s.desc)
	n := utf8.RuneCountInString(d)
	switch {
	case d == "":
		add(line, false, "description is empty")
		return
	case n > maxDescription:
		add(line, false, "description is %d characters (max %d)", n, maxDescription)
	case n > fatDescription:
		add(line, true, "description is %d characters; every agent loads it at startup, for every installed skill", n)
	case n < 30:
		add(line, true, "description is %d characters, too short to choose by", n)
	}
	if !says(d) {
		add(line, true, "description says what the skill does but not when to use it")
	}
}

// says reports whether a description tells an agent when to reach for the
// skill. The specification asks for both halves — what it does and when to
// use it — and the second half is the one authors leave out, which is
// precisely the half selection runs on.
func says(d string) bool {
	l := strings.ToLower(d)
	for _, w := range []string{"when", "use ", "used ", "using ", "load", "invoke", "trigger", "before ", "after ", "whenever", "if you", "for any"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func checkOptional(s *skill, add addFunc) {
	if s.has("license") && !scalar(s.node["license"]) {
		add(s.at("license"), false, "license must be a plain string")
	}
	if s.has("compatibility") {
		switch n := utf8.RuneCountInString(s.compat); {
		case !scalar(s.node["compatibility"]):
			add(s.at("compatibility"), false, "compatibility must be a plain string")
		case n > maxCompat:
			add(s.at("compatibility"), false, "compatibility is %d characters (max %d)", n, maxCompat)
		case n == 0:
			add(s.at("compatibility"), true, "compatibility is empty; leave it out unless the skill needs something specific")
		}
	}
	if s.has("allowed-tools") && !scalar(s.node["allowed-tools"]) {
		add(s.at("allowed-tools"), false, "allowed-tools must be one string of space-separated tools")
	}
	if s.has("metadata") {
		n := s.node["metadata"]
		if n.Kind != yaml.MappingNode {
			add(s.at("metadata"), false, "metadata must be a mapping")
		} else {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if !scalar(n.Content[i+1]) {
					add(n.Content[i].Line, false, "metadata %q must be a string; the mapping is string to string", n.Content[i].Value)
				}
			}
		}
	}
}

func scalar(n *yaml.Node) bool { return n != nil && n.Kind == yaml.ScalarNode }

// checkBody holds the rules the specification states as advice and one it
// only implies: a file reference that does not resolve. Progressive
// disclosure makes that failure invisible — the agent reads SKILL.md,
// decides it needs references/FORMS.md, and finds nothing there, which is
// a broken skill that lints clean everywhere else.
func checkBody(s *skill, dir string, add addFunc) {
	body := strings.TrimSpace(s.body)
	if body == "" {
		add(s.bodyAt, false, "no instructions: a skill with an empty body is metadata")
		return
	}
	if n := strings.Count(s.body, "\n") + 1; n > maxBodyLines {
		add(s.bodyAt, true, "the body is %d lines (the specification asks for under %d); move detail into references/", n, maxBodyLines)
	}
	if n := len(body) / 4; n > maxBodyTokens {
		add(s.bodyAt, true, "the body is roughly %d tokens (the specification asks for under %d); it is all loaded the moment the skill is chosen", n, maxBodyTokens)
	}

	prose, all := references(s.body, s.bodyAt)
	for _, r := range prose {
		if _, err := os.Stat(filepath.Join(dir, r.path)); err != nil {
			add(r.line, false, "references %s, which is not there", r.path)
			continue
		}
		if strings.Count(r.path, "/") > 1 {
			add(r.line, true, "%s is more than one level deep; keep references one level from SKILL.md", r.path)
		}
	}

	// The other half of the same rule. A bundled file nothing mentions is
	// not disclosed progressively, it is not disclosed at all: no agent
	// reads a directory it was never told about.
	mentioned := map[string]bool{}
	for _, r := range all {
		mentioned[r.path] = true
	}
	for _, sub := range []string{"scripts", "references", "assets"} {
		if !isDir(filepath.Join(dir, sub)) {
			continue
		}
		files, err := contents(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		var orphans int
		for _, f := range files {
			if !mentioned[sub+"/"+f] {
				orphans++
			}
		}
		if orphans > 0 {
			add(s.bodyAt, true, "%d file(s) in %s/ are never mentioned in SKILL.md; nothing will open them", orphans, sub)
		}
	}
}

type ref struct {
	path string
	line int
}

var (
	mdLink   = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	barePath = regexp.MustCompile("(?:^|[\\s`\"'(])((?:scripts|references|assets)/[A-Za-z0-9_./-]+)")
	fence    = regexp.MustCompile("^\\s*(```|~~~)")
)

// references pulls the files a body points at: Markdown links, and bare
// paths into the specification's own three directories, which is how its
// own example writes them.
//
// Two lists come back. The first skips fenced code, because a path in an
// example is illustration and a linter that shouts at illustrations gets
// switched off. The second includes it, because for the opposite question
// — does anything mention this file at all — a mention in a code block is
// still a mention.
func references(body string, at int) (prose, all []ref) {
	inFence := false
	for i, line := range strings.Split(body, "\n") {
		if fence.MatchString(line) {
			inFence = !inFence
			continue
		}
		for _, r := range refsIn(line, at+i) {
			all = append(all, r)
			if !inFence {
				prose = append(prose, r)
			}
		}
	}
	return prose, all
}

func refsIn(line string, at int) []ref {
	var out []ref
	seen := map[string]bool{}
	keep := func(p string) {
		p = strings.TrimSuffix(strings.Trim(p, "`\"'"), ".")
		if p == "" || seen[p] || local(p) == "" {
			return
		}
		seen[p] = true
		out = append(out, ref{path: local(p), line: at})
	}
	for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
		keep(m[1])
	}
	for _, m := range barePath.FindAllStringSubmatch(line, -1) {
		keep(m[1])
	}
	return out
}

// local returns the path a reference points to inside the skill, or "" for
// anything that is not one: a URL, an anchor, an absolute path, or a path
// that climbs out of the skill directory.
func local(p string) string {
	if p == "" || strings.Contains(p, "://") || strings.HasPrefix(p, "#") ||
		strings.HasPrefix(p, "mailto:") || strings.HasPrefix(p, "/") {
		return ""
	}
	if i := strings.IndexAny(p, "#?"); i >= 0 {
		p = p[:i]
	}
	c, err := inside(p)
	if err != nil || c == "" || !strings.Contains(c, "/") && !strings.Contains(c, ".") {
		return ""
	}
	return c
}

func mustAbs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
