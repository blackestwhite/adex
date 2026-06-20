package intel

import "testing"

func TestAnalyzeGoWithTreeSitter(t *testing.T) {
	result := Analyze("main.go", `package main
import "fmt"
type App struct{}
func main(){}
func (a *App) Run(){}
`)
	assertContains(t, result.Symbols, "type:App")
	assertContains(t, result.Symbols, "func:main")
	assertContains(t, result.Symbols, "method:Run")
	assertContains(t, result.Imports, "fmt")
	if result.Engine == "" {
		t.Fatal("expected engine")
	}
}

func TestAnalyzeJavaScript(t *testing.T) {
	result := Analyze("app.js", `const answer = 42;
function run() {}
class App {}
`)
	assertContains(t, result.Symbols, "var:answer")
	assertContains(t, result.Symbols, "func:run")
	assertContains(t, result.Symbols, "class:App")
	if result.Engine != "tree-sitter-javascript" {
		t.Fatalf("engine=%q", result.Engine)
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%q missing from %+v", want, values)
}
