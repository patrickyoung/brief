package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The disclosure invariant. Nothing but a name and a description ever
// leaves this machine. `brief find -ask` sends the catalogue — level 1, the
// same bytes `brief ls` prints — and the task, and never a body, a script,
// or a bundled file. Level 2 is read when you name a skill, level 3 when
// you name a file inside one. That is what progressive disclosure means
// when the levels are programs instead of promises, and it is the reason
// choosing a skill costs a few hundred tokens instead of a context window.

// scored is a skill and how well it matched, kept together so -v can
// explain a ranking that surprised someone.
type scored struct {
	entry
	score float64
	hits  []string
}

// rank orders the catalogue against a task by matching words, and does not
// pretend to be more than that. It is grep with a sense of proportion: a
// word that appears in one description says a great deal about which skill
// is meant, and a word that appears in all of them says nothing, so each
// match is weighted by how rare it is. A word in the skill's own name
// counts for more, because that is where authors put the noun.
//
// It is the default because it is free, instant, offline, and right about
// the cases people actually type. It cannot know that a scanned invoice is
// a PDF problem — that is what -ask is for, and it costs a model call, so
// it is a flag rather than a surprise.
func rank(cat []entry, task string) []scored {
	want := tokenize(task)
	if len(want) == 0 {
		return nil
	}
	// Document frequency over the catalogue, so weighting adapts to the
	// skills that are actually installed: "cloudflare" is a strong signal
	// among ten mixed skills and no signal at all among ten Cloudflare ones.
	df := map[string]int{}
	docs := make([]map[string]int, len(cat))
	for i, e := range cat {
		d := map[string]int{}
		for _, t := range tokenize(strings.ReplaceAll(e.name, "-", " ")) {
			d[t] = 3 // the name is the author's own summary of the skill
		}
		for _, t := range tokenize(e.desc) {
			if d[t] == 0 {
				d[t] = 1
			}
		}
		docs[i] = d
		for t := range d {
			df[t]++
		}
	}
	// A word most of the catalogue uses is not evidence about which skill
	// is meant, however much of it there is. Dropping those outright, rather
	// than letting them contribute a small score each, is the difference
	// between answering "nothing here fits" and naming whichever skill
	// happened to sort first — and a confidently wrong name is the one
	// failure the program downstream cannot detect. The list is derived from
	// the skills that are installed, not guessed at in advance; below four
	// skills there is no majority worth measuring.
	common := map[string]bool{}
	if len(cat) >= 4 {
		for t, d := range df {
			common[t] = d*2 > len(cat)
		}
	}
	n, uw := float64(len(cat)), unique(want)
	var out []scored
	for i, e := range cat {
		s := scored{entry: e}
		for _, t := range uw {
			w, ok := docs[i][t]
			if !ok || common[t] {
				continue
			}
			s.score += float64(w) * math.Log(1+n/float64(df[t]))
			s.hits = append(s.hits, t)
		}
		// A name written out in the task is not a guess about what was
		// meant. "run the web-perf skill" should not be outvoted by three
		// common words somewhere else, and it is evidence on its own.
		named := strings.Contains(" "+strings.ToLower(task)+" ", " "+e.name+" ")
		if named {
			s.score += 10
		}
		// A match has to account for a fair share of what was asked. One
		// word out of five is a coincidence, not a match — "an opened box"
		// finds a skill that says "opened" about a file, and a rare word
		// scores highly precisely because it is rare. So a single word
		// counts only when it is most of the question: "pdf" is a whole
		// task, and "opened" is one fifth of one.
		if s.score > 0 && (named || len(s.hits) > 1 || len(s.hits) == len(uw)) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].name < out[j].name // ties resolve the same way every run
	})
	return out
}

// stop is English glue and nothing else.
//
// It is short on purpose. The temptation is to add the words descriptions
// are made of — "use", "load", "build", "handle" — but that is the job
// inverse document frequency already does, and it does it better: a word
// in every description is worth nothing here without anyone deciding in
// advance that it would be, and the same word is worth a great deal in a
// catalogue where only one skill uses it. A hand-written list of domain
// words is a guess about somebody else's skills.
var stop = map[string]bool{
	"a": true, "an": true, "and": true, "any": true, "are": true, "as": true,
	"at": true, "be": true, "by": true, "can": true, "do": true, "for": true,
	"from": true, "how": true, "i": true, "if": true, "in": true, "into": true,
	"is": true, "it": true, "its": true, "me": true, "my": true, "of": true,
	"on": true, "or": true, "our": true, "so": true, "that": true,
	"the": true, "their": true, "them": true, "then": true, "there": true,
	"these": true, "they": true, "this": true, "to": true, "up": true,
	"use": true, "used": true, "using": true, "was": true, "we": true,
	"what": true, "when": true, "which": true, "will": true, "with": true,
	"you": true, "your": true,
}

// tokenize lowercases, splits on anything that is not a letter or digit,
// drops the glue, and takes a plural down to its singular so "tables"
// finds a skill that talks about a table. It is the crudest stemming there
// is, and it is applied to both sides, so it is at least consistent.
func tokenize(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) < 2 || stop[w] {
			continue
		}
		out = append(out, stem(w))
	}
	return out
}

// stem takes a word down to a crude root: the gerund a description writes
// as "charting" and a task writes as "chart", and a plural. It is applied
// to both sides, so it can only ever make two spellings of one word agree
// — the worst it can do is fail to, which is where -ask comes in.
func stem(w string) string {
	if len(w) > 5 && strings.HasSuffix(w, "ing") {
		w = w[:len(w)-3]
	}
	if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		w = w[:len(w)-1]
	}
	return w
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// askPick hands the choice to a model by running ask.
//
// brief does not link a provider, hold a key, or open a socket. It runs the
// program next to it, the way a Unix program has always borrowed a
// capability it does not own, and gets three things for free: every
// provider ask learns, every credential ask already holds, and a session
// log that makes the choice auditable afterwards. The catalogue goes on
// stdin, which is where ask takes evidence; the task goes in argv, which is
// where ask takes the question.
func askPick(cat []entry, task string, n int, quiet bool) ([]string, error) {
	if len(cat) == 0 {
		return nil, nil
	}
	var index bytes.Buffer
	for _, e := range cat {
		fmt.Fprintf(&index, "%s\t%s\n", e.name, e.desc)
	}

	// A fresh session in brief's own directory, never the caller's current
	// conversation. Choosing a skill must not become a turn in the
	// conversation the skill is about to be used for: it would answer the
	// next question with a catalogue on its mind, and `ask` would silently
	// be three turns deep in something the user never said.
	sess := filepath.Join(briefDir(), "find", sessionID()+".jsonl")
	args := []string{"-n", "-q", "-f", sess, "-S", selectPrompt(n)}
	if m := os.Getenv("BRIEF_MODEL"); m != "" {
		args = append(args, "-m", m)
	}
	args = append(args, "--", task)

	cmd := osexec.Command(askBin(), args...)
	cmd.Stdin = &index
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		// Not on $PATH, or named by a path that is not there: both mean the
		// same thing to a caller, and neither is a reason to stop working.
		if errors.Is(err, osexec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: not installed — plain `brief find` needs no model, or set $ASK", askBin())
		}
		if msg := lastLine(errb.String()); msg != "" {
			return nil, fmt.Errorf("%s: %s", askBin(), msg)
		}
		return nil, fmt.Errorf("%s: %w", askBin(), err)
	}

	// The answer is about to become a path, so it is checked against the
	// catalogue rather than trusted. A model that invents a plausible skill
	// name is a normal event; a model that invents one brief then opens is a
	// vulnerability.
	known := map[string]bool{}
	for _, e := range cat {
		known[e.name] = true
	}
	var names []string
	var unknown []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line == "none":
			continue
		case known[line]:
			if len(names) < n && !contains(names, line) {
				names = append(names, line)
			}
		default:
			unknown = append(unknown, line)
		}
	}
	if len(names) == 0 && len(unknown) > 0 {
		return nil, fmt.Errorf("%s named %q, which is not in the catalogue", askBin(), unknown[0])
	}
	if !quiet {
		what := "none"
		if len(names) > 0 {
			what = strings.Join(names, " ")
		}
		fmt.Fprintf(os.Stderr, "brief: %s · ask replay -check %s\n", what, sess)
	}
	return names, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func lastLine(s string) string {
	fields := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(fields[len(fields)-1])
}

// askBin is the program brief runs to reach a model. $ASK names another one
// — a wrapper that pins a cheap model, or the binary under test.
func askBin() string {
	if a := os.Getenv("ASK"); a != "" {
		return a
	}
	return "ask"
}

// briefDir is where brief keeps what it owns: $BRIEF_DIR, else ~/.brief. The
// only thing in it is the ask session behind each -ask choice, which is
// there so a selection can be read back months later.
func briefDir() string {
	if d := os.Getenv("BRIEF_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".brief"
	}
	return filepath.Join(home, ".brief")
}

// sessionID mints an ask session name: sortable by time, unique by chance.
// It is ask's own format, so the two tools' session directories read the
// same way when you list them.
func sessionID() string {
	var b [4]byte
	rand.Read(b[:])
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}
