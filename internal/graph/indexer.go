package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"ade-x/internal/workspace"
)

const graphBatchSize = 50

type IndexStats struct {
	Root       string
	Files      int
	Symbols    int
	Imports    int
	Statements int
}

func IndexWorkspace(ctx context.Context, client *Client, root string, maxFileBytes int64) (IndexStats, error) {
	docs, err := workspace.Collect(root, maxFileBytes)
	if err != nil {
		return IndexStats{}, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return IndexStats{}, fmt.Errorf("resolve root: %w", err)
	}

	stats := IndexStats{Root: absRoot, Files: len(docs)}
	if err := resetRepo(ctx, client, absRoot); err != nil {
		return stats, err
	}

	var batch []Statement
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		stats.Statements += len(batch)
		_, err := client.Run(ctx, batch)
		batch = batch[:0]
		return err
	}
	add := func(statement string, parameters map[string]any) error {
		batch = append(batch, Statement{Statement: statement, Parameters: parameters})
		if len(batch) >= graphBatchSize {
			return flush()
		}
		return nil
	}

	for _, doc := range docs {
		if err := add(`
MERGE (r:Repo {root: $root})
MERGE (f:File {repo: $root, path: $path})
SET f.hash = $hash, f.language = $language
MERGE (r)-[:CONTAINS]->(f)
`, map[string]any{
			"root":     absRoot,
			"path":     doc.RelPath,
			"hash":     doc.Hash,
			"language": doc.Language,
		}); err != nil {
			return stats, err
		}

		for _, symbol := range doc.Symbols {
			kind, name := splitSymbol(symbol)
			if err := add(`
MATCH (f:File {repo: $root, path: $path})
MERGE (s:Symbol {repo: $root, id: $id})
SET s.kind = $kind, s.name = $name, s.path = $path
MERGE (f)-[:DEFINES]->(s)
`, map[string]any{
				"root": absRoot,
				"path": doc.RelPath,
				"id":   doc.RelPath + "#" + symbol,
				"kind": kind,
				"name": name,
			}); err != nil {
				return stats, err
			}
			stats.Symbols++
		}

		for _, imported := range doc.Imports {
			if err := add(`
MATCH (f:File {repo: $root, path: $path})
MERGE (p:Package {name: $name})
MERGE (f)-[:IMPORTS]->(p)
`, map[string]any{
				"root": absRoot,
				"path": doc.RelPath,
				"name": imported,
			}); err != nil {
				return stats, err
			}
			stats.Imports++
		}
	}
	if err := flush(); err != nil {
		return stats, err
	}
	if _, err := client.Run(ctx, []Statement{{
		Statement: "MATCH (p:Package) WHERE NOT EXISTS { MATCH (:File)-[:IMPORTS]->(p) } DELETE p",
	}}); err != nil {
		return stats, err
	}
	return stats, nil
}

func resetRepo(ctx context.Context, client *Client, root string) error {
	_, err := client.Run(ctx, []Statement{{
		Statement: `
MATCH (r:Repo {root: $root})
OPTIONAL MATCH (r)-[:CONTAINS]->(f:File)
OPTIONAL MATCH (f)-[:DEFINES]->(s:Symbol)
DETACH DELETE s, f, r
`,
		Parameters: map[string]any{"root": root},
	}})
	return err
}

func splitSymbol(symbol string) (string, string) {
	kind, name, ok := strings.Cut(symbol, ":")
	if !ok {
		return "symbol", symbol
	}
	return kind, name
}
