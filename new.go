package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// skeleton is what `brief new` writes. It is a skill, not a form: it passes
// `brief lint -strict` on the way out, and the parts an author must replace
// are the parts that say something false about their own skill, so leaving
// one in is conspicuous rather than quietly shipped.
//
// The headings are the specification's recommended sections — steps,
// examples, edge cases — in the order a procedure is actually read.
const skeleton = `---
name: %[1]s
description: %[2]s. Use when a task involves %[1]s, or when someone asks for it by name.
---

# %[1]s

One line on what this skill is for, written for the agent about to do the
work rather than for a reader browsing a catalogue.

## Steps

1. The first thing to do, stated as an instruction.
2. The next thing.
3. What to produce, and in what shape.

## Rules

- What to do when the input is not what step 1 expected.
- What never to do, and why.

## Example

Input: a sentence describing the request as it will actually arrive.

Output: exactly what the agent should produce for it.
`

func cmdNew(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	var (
		dir   = fs.String("d", ".", "create the skill under this directory")
		force = fs.Bool("f", false, "overwrite an existing SKILL.md")
	)
	usage(fs, "brief new [flags] name")
	if err := fs.Parse(args); err != nil {
		return usageCode(fs, err)
	}
	if fs.NArg() != 1 {
		return usageErr(fs)
	}
	name := fs.Arg(0)

	// The name rules are checked here, not by lint afterwards, because a
	// skill whose directory is already wrong is a skill that has to be
	// moved to be fixed.
	switch {
	case !nameOK.MatchString(name):
		return fail(fmt.Errorf("%q is not a legal skill name: a-z, 0-9 and single hyphens, e.g. pdf-processing", name))
	case utf8.RuneCountInString(name) > maxName:
		return fail(fmt.Errorf("name is %d characters (max %d)", utf8.RuneCountInString(name), maxName))
	}

	target := filepath.Join(*dir, name)
	path := filepath.Join(target, "SKILL.md")
	if _, err := os.Stat(path); err == nil && !*force {
		return fail(fmt.Errorf("%s already exists (-f overwrites it)", path))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fail(err)
	}
	body := fmt.Sprintf(skeleton, name, title(name))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fail(err)
	}
	fmt.Println(path)
	return exitYes
}

// title turns a skill name back into the start of a sentence, so the
// description reads as English before it is edited rather than after.
func title(name string) string {
	words := strings.Split(name, "-")
	s := strings.Join(words, " ")
	return strings.ToUpper(s[:1]) + s[1:]
}
