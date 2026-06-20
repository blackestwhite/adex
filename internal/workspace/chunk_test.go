package workspace

import (
	"strings"
	"testing"
)

func TestChunkDocumentsStableID(t *testing.T) {
	doc := Document{
		RelPath:  "main.go",
		Content:  "package main\nfunc main() {}\n",
		Hash:     "abc",
		Language: "go",
	}
	first := ChunkDocuments([]Document{doc}, 20, 5)
	second := ChunkDocuments([]Document{doc}, 20, 5)
	if len(first) == 0 {
		t.Fatal("expected chunks")
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("unstable ids: %q != %q", first[0].ID, second[0].ID)
	}
	if first[0].ChunkTotal != len(first) {
		t.Fatalf("unexpected total: %+v", first[0])
	}
}

func TestChunkTextOverlaps(t *testing.T) {
	chunks := chunkText("aaa\nbbb\nccc\nddd\n", 9, 4)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks: %+v", chunks)
	}
	if chunks[0] != "aaa\nbbb" {
		t.Fatalf("first chunk = %q", chunks[0])
	}
	if !containsLine(chunks[1], "bbb") {
		t.Fatalf("second chunk should overlap previous boundary: %+v", chunks)
	}
}

func TestExtractSymbolsGo(t *testing.T) {
	symbols := ExtractSymbols("main.go", `package main
type App struct{}
func main(){}
func (a *App) Run(){}
`)
	want := map[string]bool{"type:App": true, "func:main": true, "method:App.Run": true}
	for _, symbol := range symbols {
		delete(want, symbol)
	}
	if len(want) != 0 {
		t.Fatalf("missing symbols: %+v from %+v", want, symbols)
	}
}

func TestExtractImportsGo(t *testing.T) {
	imports := ExtractImports("main.go", `package main
import (
	"fmt"
	"net/http"
)
`)
	want := map[string]bool{"fmt": true, "net/http": true}
	for _, imported := range imports {
		delete(want, imported)
	}
	if len(want) != 0 {
		t.Fatalf("missing imports: %+v from %+v", want, imports)
	}
}

func containsLine(text string, line string) bool {
	for _, current := range strings.Split(text, "\n") {
		if current == line {
			return true
		}
	}
	return false
}
