package plugincontract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The only frontmatter fields the Agent Skills spec allows. Conforming clients
// (e.g. Cursor) skip a skill with any other field rather than ignoring it.
var agentSkillsFields = map[string]bool{
	"name": true, "description": true, "license": true,
	"compatibility": true, "metadata": true, "allowed-tools": true,
}

// skillFiles returns every plugin/skills/*/SKILL.md.
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
// Only column-zero keys matter, so it is hand-parsed rather than adding YAML.
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

// `claude plugin validate --strict` does not check skill frontmatter.
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

// The spec requires `name` to match the directory.
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

// $ARGUMENTS is a Claude Code substitution other Agent Skills clients show literally.
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
