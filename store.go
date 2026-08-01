package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// entry is one line of the catalogue: level 1 of the specification's
// progressive disclosure, and the only thing lore will ever send to a
// model. The body lives on disk until something names the skill.
type entry struct {
	name string // the directory name, which is what an agent resolves by
	dir  string
	desc string
}

// lorePath returns the directories searched for skills, in order.
//
// It is $PATH, for skills: a colon-separated list, searched left to right,
// first match wins. Everyone already knows those rules, and shadowing is
// the whole point — a skill in the project overrides the one in your home
// directory, exactly the way a script in ./bin overrides /usr/bin.
//
// An empty element is skipped rather than read as the current directory.
// $PATH's rule there is a well-known way to run the wrong program, and
// there is no reason to inherit a mistake that old.
func lorePath() []string {
	if p, ok := os.LookupEnv("LORE_PATH"); ok {
		var dirs []string
		for _, d := range strings.Split(p, string(os.PathListSeparator)) {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
		return dirs
	}
	dirs := []string{filepath.Join(".claude", "skills")}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".lore", "skills"))
	}
	return dirs
}

// catalogue reads every skill on the path, nearest first, and drops the
// ones a nearer directory already answered for.
//
// A skill that will not parse is listed anyway, with an empty description.
// Hiding it would answer "why is my skill not showing up?" with silence,
// when lint answers it with a line number.
func catalogue() ([]entry, error) {
	var out []entry
	seen := map[string]bool{}
	for _, dir := range lorePath() {
		ents, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // a path element that does not exist is not an error
			}
			return nil, err
		}
		for _, de := range ents {
			name := de.Name()
			if strings.HasPrefix(name, ".") || seen[name] {
				continue
			}
			sub := filepath.Join(dir, name)
			if !isDir(sub) {
				continue
			}
			if _, err := os.Stat(filepath.Join(sub, "SKILL.md")); err != nil {
				continue // a directory without a SKILL.md is not a skill
			}
			seen[name] = true
			e := entry{name: name, dir: sub}
			if s, err := readSkill(sub); err == nil {
				e.desc = oneLine(s.desc)
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// providers returns every directory on the path that holds a skill of this
// name, nearest first. The first is the one everything else uses; the rest
// are what `lore path -a` exists to show, because a skill being shadowed
// looks exactly like a skill being ignored.
func providers(name string) []string {
	var dirs []string
	if !validName(name) {
		return nil
	}
	for _, dir := range lorePath() {
		sub := filepath.Join(dir, name)
		if isDir(sub) {
			if _, err := os.Stat(filepath.Join(sub, "SKILL.md")); err == nil {
				dirs = append(dirs, sub)
			}
		}
	}
	return dirs
}

// resolve turns a reference into a skill directory and a path inside it.
//
// A reference is a name, or a name and a path within that skill:
//
//	pdf-processing
//	pdf-processing/references/FORMS.md
//
// The first element is looked up on the path when it is a legal skill name
// — which is what makes `lore cat` safe to hand output from a model. A
// name cannot contain a dot or a slash, so it can never be ./x or ../x or
// /etc/passwd, and the remainder is checked against escaping the skill
// directory besides.
//
// Anything that is not a legal name is a filesystem path, and then the
// skill is the nearest directory at or above it holding a SKILL.md. That
// is what lets `lore lint .` and `lore cat ./draft/SKILL.md` work in a
// directory you are still writing.
func resolve(ref string) (dir, rel string, err error) {
	if ref == "" {
		return "", "", errors.New("empty reference")
	}
	head, rest, _ := strings.Cut(ref, "/")
	if validName(head) {
		if dirs := providers(head); len(dirs) > 0 {
			if rel, err := inside(rest); err == nil {
				return dirs[0], rel, nil
			} else {
				return "", "", fmt.Errorf("%s: %w", ref, err)
			}
		}
	}
	if !strings.ContainsAny(ref, "/.") {
		return "", "", fmt.Errorf("no skill %q on %s", ref, strings.Join(lorePath(), string(os.PathListSeparator)))
	}
	return locate(ref)
}

// locate finds the skill containing a filesystem path: the nearest
// directory at or above it with a SKILL.md in it.
func locate(path string) (dir, rel string, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", "", fmt.Errorf("%s: no such skill or path", path)
	}
	d := abs
	if !isDir(d) {
		d = filepath.Dir(d)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err == nil {
			r, err := filepath.Rel(d, abs)
			if err != nil {
				return "", "", err
			}
			if r == "." {
				r = ""
			}
			return d, filepath.ToSlash(r), nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", "", fmt.Errorf("%s: no SKILL.md here or above it", path)
		}
		d = parent
	}
}

// inside checks that a path within a skill stays within it. The reference
// may have come from a model, and a model that invents a plausible
// filename must not be able to invent ../../.ssh/id_rsa with it.
func inside(rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	if filepath.IsAbs(rel) {
		return "", errors.New("a path inside a skill cannot be absolute")
	}
	c := filepath.ToSlash(filepath.Clean(rel))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", errors.New("a path inside a skill cannot leave it")
	}
	return c, nil
}

// contents lists the files a skill bundles — level 3 — as paths relative
// to the skill, sorted, so `lore ls name` names exactly what `lore cat
// name/<path>` will read. Symbolic links are listed but not followed:
// walking them turns a catalogue into an unbounded search of a filesystem
// somebody else laid out.
func contents(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != dir && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		r, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(r))
		return nil
	})
	sort.Strings(out)
	return out, err
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// oneLine flattens a description to a single line, because the catalogue
// is one skill per line and a description that was written as a YAML block
// scalar would otherwise turn one skill into several.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
