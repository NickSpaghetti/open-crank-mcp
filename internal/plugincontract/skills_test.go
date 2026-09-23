package plugincontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The only frontmatter fields the Agent Skills spec allows. Everything else is a
// client extension, and a conforming client skips the whole skill rather than
// ignoring the field - measured on Cursor, which dropped four of these five.
var agentSkillsFields = map[string]bool{
	"name": true, "description": true, "license": true,
	"compatibility": true, "metadata": true, "allowed-tools": true,
}

// skillFiles returns every plugin/skills/*/SKILL.md, which is the fixed location
// the 1.0.0 spec discovers skills from.
func skillFiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(repoRoot, "plugin", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); err == nil {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no SKILL.md found under %s", dir)
	}
	return found
}

// readSkill splits a SKILL.md into its top-level frontmatter keys and its body.
//
// Hand-parsed rather than with a YAML library, because the repo has no YAML
// dependency and this needs only the keys at column zero: an indented line
// belongs to whatever key opened the block, and `metadata` is allowed to nest.
func readSkill(t *testing.T, path string) (keys []string, body string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		t.Fatalf("%s does not open with a --- frontmatter fence", path)
	}
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return keys, strings.Join(lines[i+2:], "\n")
		}
		// Indented lines are values inside the key above them, not keys.
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue
		}
		if name, _, found := strings.Cut(line, ":"); found {
			keys = append(keys, strings.TrimSpace(name))
		}
	}
	t.Fatalf("%s has no closing --- frontmatter fence", path)
	return nil, ""
}

// The check that would have caught the shipped bug: four skills carried
// disable-model-invocation, so every client but Claude Code silently loaded one
// skill out of five. `claude plugin validate --strict` does not look at skill
// frontmatter, so nothing else in the toolchain sees this.
func TestSkillsCarryOnlyAgentSkillsFields(t *testing.T) {
	for _, path := range skillFiles(t) {
		keys, _ := readSkill(t, path)
		for _, key := range keys {
			if !agentSkillsFields[key] {
				t.Errorf("%s declares %q, which the Agent Skills spec does not allow. "+
					"A conforming client skips the whole skill rather than the field, so "+
					"this makes the skill vanish everywhere except Claude Code. Claude "+
					"Code reads it only at the top level - nesting it under \"metadata\" "+
					"is silently ignored rather than honoured.",
					strings.TrimPrefix(path, repoRoot+"/"), key)
			}
		}
	}
}

// The spec requires `name` to match the directory, and a rename is where the two
// drift: the directory moves, the frontmatter does not, and the skill loads under
// a name nothing references.
func TestSkillNamesMatchTheirDirectories(t *testing.T) {
	for _, path := range skillFiles(t) {
		dir := filepath.Base(filepath.Dir(path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var name string
		for _, line := range strings.Split(string(b), "\n") {
			if rest, found := strings.CutPrefix(line, "name:"); found {
				name = strings.TrimSpace(rest)
				break
			}
		}
		if name != dir {
			t.Errorf("plugin/skills/%s/SKILL.md declares name %q; the Agent Skills spec "+
				"requires it to match the directory", dir, name)
		}
	}
}

// $ARGUMENTS is a Claude Code substitution with no Agent Skills equivalent, so a
// body using it loads on a conforming client and then shows the model a literal
// dollar sign. Loading is not the same as working.
func TestSkillBodiesUseNoClaudeSubstitutions(t *testing.T) {
	for _, path := range skillFiles(t) {
		_, body := readSkill(t, path)
		if strings.Contains(body, "$ARGUMENTS") {
			t.Errorf("%s uses $ARGUMENTS, which only Claude Code expands. Take the value "+
				"from what the user asked for instead, so the skill reads the same "+
				"everywhere.", strings.TrimPrefix(path, repoRoot+"/"))
		}
	}
}
