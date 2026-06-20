package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Source struct {
	Path    string
	Content string
}

func Load(root string) ([]Source, error) {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".adex", "AGENTS.md"))
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	paths = append(paths, filepath.Join(absRoot, "AGENTS.md"))

	var sources []Source
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if strings.TrimSpace(string(raw)) == "" {
			continue
		}
		sources = append(sources, Source{Path: path, Content: string(raw)})
	}
	return sources, nil
}

func Format(sources []Source) string {
	if len(sources) == 0 {
		return ""
	}
	var out strings.Builder
	for _, source := range sources {
		out.WriteString("## ")
		out.WriteString(source.Path)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(source.Content))
		out.WriteString("\n\n")
	}
	return strings.TrimSpace(out.String())
}
