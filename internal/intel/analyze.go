package intel

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	treeSitterGo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	treeSitterJS "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
)

type Result struct {
	Symbols []string
	Imports []string
	Engine  string
}

func Analyze(relPath string, content string) Result {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".go":
		result := analyzeTreeSitter(relPath, content, sitter.NewLanguage(treeSitterGo.Language()))
		result.Imports = extractGoImports(relPath, content)
		if len(result.Symbols) == 0 {
			result.Symbols = extractGoSymbols(relPath, content)
			result.Engine = "go/parser"
		}
		if result.Engine == "" {
			result.Engine = "tree-sitter-go"
		}
		return result
	case ".js", ".jsx", ".ts", ".tsx":
		result := analyzeTreeSitter(relPath, content, sitter.NewLanguage(treeSitterJS.Language()))
		if result.Engine == "" {
			result.Engine = "tree-sitter-javascript"
		}
		return result
	default:
		return Result{}
	}
}

func analyzeTreeSitter(relPath string, content string, language *sitter.Language) Result {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return Result{}
	}
	tree := parser.Parse([]byte(content), nil)
	if tree == nil {
		return Result{}
	}
	defer tree.Close()

	var symbols []string
	visit(tree.RootNode(), []byte(content), func(node *sitter.Node) {
		if symbol, ok := symbolFromNode(node, []byte(content)); ok {
			symbols = append(symbols, symbol)
		}
	})
	sort.Strings(symbols)
	symbols = unique(symbols)
	if len(symbols) == 0 {
		return Result{}
	}
	return Result{Symbols: symbols, Engine: engineFor(relPath)}
}

func visit(node *sitter.Node, content []byte, fn func(*sitter.Node)) {
	if node == nil {
		return
	}
	fn(node)
	count := node.NamedChildCount()
	for i := uint(0); i < count; i++ {
		visit(node.NamedChild(i), content, fn)
	}
}

func symbolFromNode(node *sitter.Node, content []byte) (string, bool) {
	switch node.Kind() {
	case "function_declaration":
		return namedSymbol("func", node, content)
	case "method_declaration", "method_definition":
		return namedSymbol("method", node, content)
	case "type_declaration":
		return goTypeSymbol(node, content)
	case "class_declaration":
		return namedSymbol("class", node, content)
	case "lexical_declaration", "variable_declaration", "const_declaration":
		return declarationSymbol(node, content)
	default:
		return "", false
	}
}

func namedSymbol(kind string, node *sitter.Node, content []byte) (string, bool) {
	name := node.ChildByFieldName("name")
	if name == nil {
		return "", false
	}
	text := strings.TrimSpace(name.Utf8Text(content))
	if text == "" {
		return "", false
	}
	return kind + ":" + text, true
}

func goTypeSymbol(node *sitter.Node, content []byte) (string, bool) {
	visitTarget := node
	for i := uint(0); i < visitTarget.NamedChildCount(); i++ {
		child := visitTarget.NamedChild(i)
		if child.Kind() == "type_spec" {
			name := child.ChildByFieldName("name")
			if name != nil {
				text := strings.TrimSpace(name.Utf8Text(content))
				if text != "" {
					return "type:" + text, true
				}
			}
		}
	}
	return "", false
}

func declarationSymbol(node *sitter.Node, content []byte) (string, bool) {
	var found string
	visit(node, content, func(child *sitter.Node) {
		if found != "" || child.Kind() != "variable_declarator" {
			return
		}
		name := child.ChildByFieldName("name")
		if name == nil {
			return
		}
		text := strings.TrimSpace(name.Utf8Text(content))
		if text != "" {
			found = "var:" + text
		}
	})
	return found, found != ""
}

func extractGoImports(relPath string, content string) []string {
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

func extractGoSymbols(relPath string, content string) []string {
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
	return unique(symbols)
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

func engineFor(relPath string) string {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".go":
		return "tree-sitter-go"
	case ".js", ".jsx", ".ts", ".tsx":
		return "tree-sitter-javascript"
	default:
		return "unknown"
	}
}

func unique(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	var last string
	for i, value := range values {
		if i == 0 || value != last {
			out = append(out, value)
			last = value
		}
	}
	return out
}

func DebugTree(relPath string, content string) (string, error) {
	var language *sitter.Language
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".go":
		language = sitter.NewLanguage(treeSitterGo.Language())
	case ".js", ".jsx", ".ts", ".tsx":
		language = sitter.NewLanguage(treeSitterJS.Language())
	default:
		return "", fmt.Errorf("unsupported language")
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return "", err
	}
	tree := parser.Parse([]byte(content), nil)
	if tree == nil {
		return "", fmt.Errorf("parse failed")
	}
	defer tree.Close()
	return tree.RootNode().ToSexp(), nil
}
