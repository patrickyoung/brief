package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInsideRefusesToLeave. This is the whole of the sandbox: a path
// inside a skill is a path inside a skill.
func TestInsideRefusesToLeave(t *testing.T) {
	for _, ok := range []string{"", "SKILL.md", "references/FORMS.md", "a/b/c.txt", "./SKILL.md"} {
		if _, err := inside(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"..", "../x", "a/../..", "/etc/passwd", "a/../../b"} {
		if _, err := inside(bad); err == nil {
			t.Errorf("%q was allowed", bad)
		}
	}
}

// TestResolveFallsBackToTheFilesystem, so lint and cat work on a skill you
// are still writing, which is not installed anywhere yet.
func TestResolveFallsBackToTheFilesystem(t *testing.T) {
	dir := tree(t, map[string]string{
		"draft/SKILL.md":            skillFile("draft", "A draft. Use when drafting.", "body"),
		"draft/references/NOTES.md": "notes\n",
	})
	withPath(t, t.TempDir()) // nothing installed

	skillDir := filepath.Join(dir, "draft")
	for _, tc := range []struct{ ref, wantRel string }{
		{skillDir, ""},
		{filepath.Join(skillDir, "SKILL.md"), "SKILL.md"},
		{filepath.Join(skillDir, "references", "NOTES.md"), "references/NOTES.md"},
	} {
		got, rel, err := resolve(tc.ref)
		if err != nil {
			t.Fatalf("%s: %v", tc.ref, err)
		}
		if got != mustAbs(skillDir) || rel != tc.wantRel {
			t.Fatalf("%s -> %s %q", tc.ref, got, rel)
		}
	}
	if _, _, err := resolve(filepath.Join(dir, "nowhere")); err == nil {
		t.Fatal("resolved a path that is not there")
	}
}

// TestResolvePrefersTheCatalogue. A bare word is a name, and a name is
// never a path — which is what lets output from a model be handed
// straight to cat.
func TestResolvePrefersTheCatalogue(t *testing.T) {
	installed := tree(t, map[string]string{
		"draft/SKILL.md": skillFile("draft", "The installed one, used when both exist.", "installed"),
	})
	withPath(t, installed)

	// A directory named draft sits in the working directory too. The name
	// still resolves to the installed skill, because a name is not a path.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	local := tree(t, map[string]string{
		"draft/SKILL.md": skillFile("draft", "The local one, used when it is alone.", "local"),
	})
	if err := os.Chdir(local); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })

	dir, _, err := resolve("draft")
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(installed, "draft") {
		t.Fatalf("resolved to %s", dir)
	}
	// Spelling it as a path is how you ask for the one in front of you.
	if dir, _, err := resolve("./draft"); err != nil || dir != mustAbs("draft") {
		t.Fatalf("./draft -> %s %v", dir, err)
	}
}

// TestCatalogueSkipsWhatIsNotASkill. A directory is a skill when it holds
// a SKILL.md, and not before.
func TestCatalogueSkipsWhatIsNotASkill(t *testing.T) {
	dir := tree(t, map[string]string{
		"real/SKILL.md":    skillFile("real", "A real one. Use when real.", "body"),
		"empty/README.md":  "nothing here\n",
		".hidden/SKILL.md": skillFile("hidden", "Hidden. Use when hidden.", "body"),
		"loose-file.md":    "not a directory\n",
	})
	withPath(t, dir)
	cat, err := catalogue()
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 1 || cat[0].name != "real" {
		t.Fatalf("catalogue %+v", cat)
	}
}

// TestCatalogueListsASkillItCannotParse. Hiding a broken skill answers
// "why is my skill not showing up?" with silence; lint answers it with a
// line number, and it cannot do that for something ls never mentioned.
func TestCatalogueListsASkillItCannotParse(t *testing.T) {
	dir := tree(t, map[string]string{"broken/SKILL.md": "# no frontmatter at all\n"})
	withPath(t, dir)
	cat, err := catalogue()
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 1 || cat[0].name != "broken" || cat[0].desc != "" {
		t.Fatalf("catalogue %+v", cat)
	}
}

// TestPathElementThatIsNotThereIsNotAnError. A default search path names
// directories most people do not have, and a program that failed on that
// would be unusable out of the box.
func TestPathElementThatIsNotThereIsNotAnError(t *testing.T) {
	real := tree(t, map[string]string{"real/SKILL.md": skillFile("real", "A real one. Use when real.", "body")})
	withPath(t, filepath.Join(t.TempDir(), "nope"), real)
	cat, err := catalogue()
	if err != nil || len(cat) != 1 {
		t.Fatalf("%v %+v", err, cat)
	}
	// An empty element is skipped rather than read as the working
	// directory: $PATH's oldest footgun is not worth inheriting.
	t.Setenv("LORE_PATH", string(os.PathListSeparator)+real)
	if dirs := lorePath(); len(dirs) != 1 || dirs[0] != real {
		t.Fatalf("lorePath %v", dirs)
	}
}

// TestDefaultPathIsTheOneDocumented. The default is a promise in the help
// text, and the help text is tested against nothing else.
func TestDefaultPathIsTheOneDocumented(t *testing.T) {
	os.Unsetenv("LORE_PATH")
	dirs := lorePath()
	if len(dirs) == 0 || dirs[0] != filepath.Join(".claude", "skills") {
		t.Fatalf("default path %v", dirs)
	}
	for _, d := range dirs {
		if !strings.Contains(usageText, strings.TrimPrefix(filepath.ToSlash(d), homeSlash())) {
			t.Errorf("the help text does not name %s", d)
		}
	}
}

func homeSlash() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.ToSlash(h) + "/"
}

// TestContentsListsEveryBundledFile, sorted, so two runs agree and a
// listing can be diffed.
func TestContentsListsEveryBundledFile(t *testing.T) {
	dir := tree(t, map[string]string{
		"s/SKILL.md":           "x",
		"s/scripts/run.py":     "x",
		"s/references/A.md":    "x",
		"s/assets/tpl/one.txt": "x",
		"s/.hidden/secret":     "x",
	})
	got, err := contents(filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	want := "SKILL.md assets/tpl/one.txt references/A.md scripts/run.py"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %q want %q", strings.Join(got, " "), want)
	}
}
