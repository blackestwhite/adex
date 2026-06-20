package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"ade-x/internal/intel"
)

const DefaultMaxFileBytes = 512 * 1024

type Document struct {
	Path     string
	RelPath  string
	Content  string
	Hash     string
	Language string
	Symbols  []string
	Imports  []string
	Engine   string
}

func Collect(root string, maxFileBytes int64) ([]Document, error) {
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultMaxFileBytes
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}

	var docs []Document
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !shouldIndexFile(name) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 || info.Size() > maxFileBytes {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !looksText(raw) {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hashBytes := sha256.Sum256(raw)
		content := string(raw)
		analysis := intel.Analyze(rel, content)
		docs = append(docs, Document{
			Path:     path,
			RelPath:  rel,
			Content:  content,
			Hash:     hex.EncodeToString(hashBytes[:]),
			Language: languageFor(name),
			Symbols:  analysis.Symbols,
			Imports:  analysis.Imports,
			Engine:   analysis.Engine,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk workspace: %w", err)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].RelPath < docs[j].RelPath })
	return docs, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".vscode", ".adex", "node_modules", "vendor", "dist", "build", "coverage", ".next", ".nuxt", ".output", "tmp", "temp":
		return true
	default:
		return strings.HasPrefix(name, ".cache")
	}
}

func shouldIndexFile(name string) bool {
	if strings.HasPrefix(name, ".") && name != ".env.example" && name != ".gitignore" {
		return false
	}
	switch name {
	case "AGENTS.md", "README.md", "Makefile", "Dockerfile", "docker-compose.yml", "go.mod", "go.sum", "package.json", "pnpm-lock.yaml", "yarn.lock", "tsconfig.json":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".vue", ".svelte", ".py", ".rb", ".rs", ".java", ".kt", ".c", ".h", ".cc", ".cpp", ".cs", ".php", ".sql", ".sh", ".bash", ".zsh", ".fish", ".md", ".txt", ".yaml", ".yml", ".json", ".toml", ".ini", ".env", ".css", ".scss", ".html", ".xml", ".proto", ".graphql":
		return true
	default:
		return false
	}
}

func looksText(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	if !utf8.Valid(raw) {
		return false
	}
	limit := len(raw)
	if limit > 4096 {
		limit = 4096
	}
	for _, b := range raw[:limit] {
		if b == 0 {
			return false
		}
	}
	return true
}

func languageFor(name string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "" {
		return strings.ToLower(name)
	}
	return ext
}
