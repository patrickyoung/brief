package main

import (
	"fmt"
	"strings"
)

// selectPrompt is the system prompt `brief find -ask` sends. It is a value,
// not a secret: `brief prompt` prints the exact built-in text for inspection
// or for callers that assemble their own Ask selection boundary.
//
// Everything in it follows from one fact: the model is being handed a
// catalogue and must answer with a name from it. It is not being asked to
// do the task, or to explain, or to be helpful about a task no skill
// covers. The answer is about to be a filename.
func selectPrompt(n int) string {
	limit := "Print at most one name."
	if n > 1 {
		limit = fmt.Sprintf("Print at most %d names, best first, one per line.", n)
	}
	return strings.Join([]string{
		"You are choosing which skill, if any, fits a task.",
		"",
		"A skill is a written procedure an agent loads before doing work. The first text file block is the catalogue: one skill per line, a name, a tab, then the description its author wrote — what it does and when to use it. The task follows as user text.",
		"",
		"Answer with names from the catalogue, spelled exactly as they appear there, one per line, and nothing else. No numbering, no quotes, no explanation, no trailing prose. " + limit,
		"",
		"If nothing in the catalogue fits, answer with the single word: none",
		"",
		"Choose by what the task needs done, not by words it shares with a description. A scanned invoice needs the PDF skill though it never says PDF. A task that mentions Kubernetes only to say where the log came from needs no Kubernetes skill.",
		"",
		"none is a real answer and often the right one. A skill loaded for a task it does not fit costs the agent its context and points it at the wrong procedure, which is worse than no skill at all.",
	}, "\n")
}
