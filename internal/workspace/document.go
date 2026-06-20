package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
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
		docs = append(docs, Document{
			Path:     path,
			RelPath:  rel,
			Content:  content,
			Hash:     hex.EncodeToString(hashBytes[:]),
			Language: languageFor(name),
			Symbols:  ExtractSymbols(rel, content),
			Imports:  ExtractImports(rel, content),
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

func ExtractImports(relPath string, content string) []string {
	if filepath.Ext(relPath) != ".go" {
		return nil
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, relPath, content, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		imports = append(imports, path)
	}
	sort.Strings(imports)
	return imports
}

func ExtractSymbols(relPath string, content string) []string {
	if filepath.Ext(relPath) != ".go" {
		return nil
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, relPath, content, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var symbols []string
	for _, decl := range file.Decls {
		switch node := decl.(type) {
		case *ast.FuncDecl:
			if node.Recv != nil && len(node.Recv.List) > 0 {
				symbols = append(symbols, "method:"+recvName(node.Recv.List[0].Type)+"."+node.Name.Name)
			} else {
				symbols = append(symbols, "func:"+node.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				switch typed := spec.(type) {
				case *ast.TypeSpec:
					symbols = append(symbols, "type:"+typed.Name.Name)
				case *ast.ValueSpec:
					for _, name := range typed.Names {
						if name.Name != "_" {
							symbols = append(symbols, strings.ToLower(node.Tok.String())+":"+name.Name)
						}
					}
				}
			}
		}
	}
	sort.Strings(symbols)
	return symbols
}

func recvName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return recvName(node.X)
	case *ast.SelectorExpr:
		return node.Sel.Name
	default:
		return "receiver"
	}
}
