package slashcommands

import (
	"bufio"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistantskills"
	"github.com/sunfayao/persona/server/internal/proto"
	"gopkg.in/yaml.v3"
)

func ClaudeCommands(cwd, profile, home string) []proto.SlashCommand {
	var rows []proto.SlashCommand
	for dir := range projectParents(cwd, home) {
		rows = append(rows, markdownCommands(filepath.Join(dir, ".claude", "commands"))...)
	}
	return append(rows, markdownCommands(filepath.Join(profile, "commands"))...)
}

func ClaudeSkills(cwd, profile, home string) ([]assistantskills.Entry, error) {
	var roots []assistantskills.Root
	for dir := range projectParents(cwd, home) {
		roots = append(roots, assistantskills.Root{Path: filepath.Join(dir, ".claude", "skills"), Source: "project"})
	}
	roots = append(roots, assistantskills.Root{Path: filepath.Join(profile, "skills"), Source: "user"})
	return assistantskills.LocalCatalog(roots)
}

func projectParents(cwd, home string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for dir, depth := cwd, 0; depth < 64 && dir != home; depth++ {
			if !yield(dir) {
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir || isRepositoryRoot(dir) {
				return
			}
			dir = parent
		}
	}
}

func markdownCommands(root string) []proto.SlashCommand {
	var rows []proto.SlashCommand
	visited := 0
	// Claude Code interprets command filenames as invocation names.
	_ = filepath.WalkDir(root, func(path string, ent os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 4096 || len(rows) >= 256 {
			return filepath.SkipAll
		}
		if ent.IsDir() {
			if path != root && strings.HasPrefix(ent.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !ent.Type().IsRegular() || filepath.Ext(path) != ".md" || strings.HasPrefix(ent.Name(), ".") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		meta, visible := commandDescription(f)
		_ = f.Close()
		if !visible {
			return nil
		}
		name := strings.TrimSuffix(ent.Name(), ".md")
		rows = append(rows, proto.SlashCommand{Name: name, Description: meta, Source: proto.SlashCustom})
		return nil
	})
	return rows
}

func commandDescription(r io.Reader) (string, bool) {
	lines := bufio.NewScanner(io.LimitReader(r, 16<<10))
	if !lines.Scan() {
		return "", true
	}
	if strings.TrimSpace(lines.Text()) != "---" {
		return "", true
	}
	var header strings.Builder
	for lines.Scan() && strings.TrimSpace(lines.Text()) != "---" {
		header.WriteString(lines.Text() + "\n")
	}
	var meta struct {
		Description string
		Invocable   *bool `yaml:"user-invocable"`
	}
	if err := yaml.Unmarshal([]byte(header.String()), &meta); err != nil {
		return "", false
	}
	return meta.Description, meta.Invocable == nil || *meta.Invocable
}

func isRepositoryRoot(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
