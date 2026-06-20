package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ade-x/internal/config"
	"ade-x/internal/graph"
	"ade-x/internal/indexstate"
	"ade-x/internal/instructions"
	"ade-x/internal/ollama"
	"ade-x/internal/qdrant"
	"ade-x/internal/workspace"
)

const (
	DefaultTopK      = 8
	DefaultAskTopK   = 2
	DefaultBatchSize = 16
	MaxContextChars  = 6000
)

type Runtime struct {
	cfg    config.Config
	llm    *ollama.Client
	memory *qdrant.Client
	graph  *graph.Client
}

type IndexStats struct {
	Root        string
	Documents   int
	Chunks      int
	Embeddings  int
	Collection  string
	VectorSize  int
	Instruction int
	Changed     int
	Deleted     int
	Skipped     int
	Full        bool
}

type RetrievedChunk struct {
	Path       string
	Content    string
	Hash       string
	Language   string
	Symbols    []string
	Engine     string
	ChunkIndex int
	ChunkTotal int
	Score      float64
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	llm, err := ollama.New(cfg.OllamaURL)
	if err != nil {
		return nil, err
	}
	memory, err := qdrant.New(cfg.QdrantURL)
	if err != nil {
		return nil, err
	}
	codeGraph, err := graph.New(cfg.Neo4jURL, cfg.Neo4jUser, cfg.Neo4jPass)
	if err != nil {
		return nil, err
	}
	return &Runtime{cfg: cfg, llm: llm, memory: memory, graph: codeGraph}, nil
}

func (r *Runtime) Doctor(ctx context.Context) error {
	if err := r.memory.Health(ctx); err != nil {
		return err
	}
	embed, err := r.llm.Embed(ctx, r.cfg.EmbedModel, []string{"doctor"})
	if err != nil {
		return err
	}
	if len(embed.Embeddings) != 1 || len(embed.Embeddings[0]) == 0 {
		return fmt.Errorf("ollama embed returned no vector")
	}
	chat, err := r.llm.Chat(ctx, r.cfg.ChatModel, []ollama.Message{{
		Role:    "user",
		Content: "Reply with exactly: ok",
	}})
	if err != nil {
		return err
	}
	if strings.TrimSpace(strings.ToLower(chat.Message.Content)) == "" {
		return fmt.Errorf("ollama chat returned empty response")
	}
	return nil
}

func (r *Runtime) Index(ctx context.Context, root string, maxFileBytes int64, chunkBytes int, chunkOverlapBytes int, batchSize int, full bool) (IndexStats, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	absRoot := mustAbs(root)
	docs, err := workspace.Collect(root, maxFileBytes)
	if err != nil {
		return IndexStats{}, err
	}
	store, err := indexstate.Open(absRoot)
	if err != nil {
		return IndexStats{}, err
	}
	defer store.Close()
	previous, err := store.Load(ctx)
	if err != nil {
		return IndexStats{}, err
	}
	if len(previous) == 0 {
		full = true
	}
	current := map[string]workspace.Document{}
	for _, doc := range docs {
		current[doc.RelPath] = doc
	}
	targetDocs, deleted := indexPlan(docs, previous, current, full)
	chunks := workspace.ChunkDocuments(targetDocs, chunkBytes, chunkOverlapBytes)
	stats := IndexStats{
		Root:       absRoot,
		Documents:  len(docs),
		Chunks:     len(chunks),
		Collection: r.cfg.Collection,
		Changed:    len(targetDocs),
		Deleted:    len(deleted),
		Skipped:    len(docs) - len(targetDocs),
		Full:       full,
	}
	if full {
		stats.Skipped = 0
	}
	if len(chunks) == 0 {
		if full {
			if err := r.memory.RecreateCollection(ctx, r.cfg.Collection, 1); err != nil {
				return stats, err
			}
			if err := store.Replace(ctx, fileStates(docs)); err != nil {
				return stats, err
			}
			stats.VectorSize = 1
			return stats, nil
		}
		if len(deleted) > 0 {
			if err := r.memory.DeletePaths(ctx, r.cfg.Collection, deleted); err != nil {
				return stats, err
			}
			if err := store.Apply(ctx, nil, deleted); err != nil {
				return stats, err
			}
		}
		return stats, nil
	}

	for start := 0; start < len(chunks); start += batchSize {
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[start:end]
		inputs := make([]string, 0, len(batch))
		for _, chunk := range batch {
			inputs = append(inputs, embedText(chunk))
		}
		embed, err := r.llm.Embed(ctx, r.cfg.EmbedModel, inputs)
		if err != nil {
			return stats, fmt.Errorf("embed batch %d-%d: %w", start, end, err)
		}
		if len(embed.Embeddings) != len(batch) {
			return stats, fmt.Errorf("embedding count mismatch: got %d want %d", len(embed.Embeddings), len(batch))
		}
		if stats.VectorSize == 0 {
			stats.VectorSize = len(embed.Embeddings[0])
			if full {
				if err := r.memory.RecreateCollection(ctx, r.cfg.Collection, stats.VectorSize); err != nil {
					return stats, err
				}
			} else {
				if err := r.memory.EnsureCollection(ctx, r.cfg.Collection, stats.VectorSize); err != nil {
					return stats, err
				}
				paths := append(documentPaths(targetDocs), deleted...)
				if err := r.memory.DeletePaths(ctx, r.cfg.Collection, paths); err != nil {
					return stats, err
				}
			}
		}
		points := make([]qdrant.Point, 0, len(batch))
		for i, chunk := range batch {
			points = append(points, qdrant.Point{
				ID:     chunk.ID,
				Vector: embed.Embeddings[i],
				Payload: map[string]any{
					"path":        chunk.Path,
					"content":     chunk.Content,
					"hash":        chunk.Hash,
					"language":    chunk.Language,
					"symbols":     chunk.Symbols,
					"engine":      chunk.Engine,
					"chunk_index": chunk.ChunkIndex,
					"chunk_total": chunk.ChunkTotal,
					"kind":        "code",
				},
			})
		}
		if err := r.memory.Upsert(ctx, r.cfg.Collection, points); err != nil {
			return stats, err
		}
		stats.Embeddings += len(batch)
	}
	if full {
		if err := store.Replace(ctx, fileStates(docs)); err != nil {
			return stats, err
		}
	} else {
		if err := store.Apply(ctx, fileStates(targetDocs), deleted); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func indexPlan(docs []workspace.Document, previous map[string]string, current map[string]workspace.Document, full bool) ([]workspace.Document, []string) {
	if full {
		return docs, nil
	}
	var changed []workspace.Document
	for _, doc := range docs {
		if previous[doc.RelPath] != doc.Hash {
			changed = append(changed, doc)
		}
	}
	var deleted []string
	for path := range previous {
		if _, ok := current[path]; !ok {
			deleted = append(deleted, path)
		}
	}
	return changed, deleted
}

func fileStates(docs []workspace.Document) []indexstate.FileState {
	states := make([]indexstate.FileState, 0, len(docs))
	for _, doc := range docs {
		states = append(states, indexstate.FileState{Path: doc.RelPath, Hash: doc.Hash})
	}
	return states
}

func documentPaths(docs []workspace.Document) []string {
	paths := make([]string, 0, len(docs))
	for _, doc := range docs {
		paths = append(paths, doc.RelPath)
	}
	return paths
}

func (r *Runtime) Search(ctx context.Context, query string, topK int) ([]RetrievedChunk, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if topK <= 0 {
		topK = DefaultTopK
	}
	embed, err := r.llm.Embed(ctx, r.cfg.EmbedModel, []string{query})
	if err != nil {
		return nil, err
	}
	if len(embed.Embeddings) != 1 {
		return nil, fmt.Errorf("ollama returned %d query embeddings", len(embed.Embeddings))
	}
	results, err := r.memory.Search(ctx, r.cfg.Collection, embed.Embeddings[0], topK)
	if err != nil {
		return nil, err
	}
	retrieved := make([]RetrievedChunk, 0, len(results))
	seen := map[string]bool{}
	for _, result := range results {
		chunk := RetrievedChunk{
			Path:       payloadString(result.Payload, "path"),
			Content:    payloadString(result.Payload, "content"),
			Hash:       payloadString(result.Payload, "hash"),
			Language:   payloadString(result.Payload, "language"),
			Symbols:    payloadStrings(result.Payload, "symbols"),
			Engine:     payloadString(result.Payload, "engine"),
			ChunkIndex: payloadInt(result.Payload, "chunk_index"),
			ChunkTotal: payloadInt(result.Payload, "chunk_total"),
			Score:      result.Score,
		}
		key := chunk.Path + ":" + strconv.Itoa(chunk.ChunkIndex)
		if seen[key] {
			continue
		}
		seen[key] = true
		retrieved = append(retrieved, chunk)
	}
	return retrieved, nil
}

func (r *Runtime) Ask(ctx context.Context, root string, question string, topK int, patchMode bool) (string, []RetrievedChunk, error) {
	chunks, err := r.Search(ctx, question, topK)
	if err != nil {
		return "", nil, err
	}
	sources, err := instructions.Load(root)
	if err != nil {
		return "", nil, err
	}
	pinned := loadPinnedContext(root)
	graphContext := r.graphContext(ctx, root, append(pinned, chunks...))
	prompt := buildPrompt(question, pinned, chunks, graphContext, sources, patchMode)
	response, err := r.llm.Chat(ctx, r.cfg.ChatModel, []ollama.Message{
		{
			Role:    "system",
			Content: systemPrompt(patchMode),
		},
		{
			Role:    "user",
			Content: prompt,
		},
	})
	if err != nil {
		return "", nil, err
	}
	answer := strings.TrimSpace(response.Message.Content)
	if patchMode {
		answer = stripPatchFences(answer)
	}
	if !patchMode && isJunkAnswer(answer, question) {
		retryPrompt := buildRetryPrompt(question, pinned, chunks)
		response, err = r.llm.Chat(ctx, r.cfg.ChatModel, []ollama.Message{
			{
				Role:    "system",
				Content: "You are Adex, a local coding assistant. Answer the user's coding question directly. Use file paths from context. Never answer with one word.",
			},
			{
				Role:    "user",
				Content: retryPrompt,
			},
		})
		if err != nil {
			return "", chunks, err
		}
		answer = strings.TrimSpace(response.Message.Content)
	}
	if !patchMode && isJunkAnswer(answer, question) {
		return "", chunks, fmt.Errorf("model returned unusable answer %q; try `adex ask -k 4 ...` after `adex index -root .`", answer)
	}
	if patchMode && answer == "" {
		return "", chunks, fmt.Errorf("model returned empty patch response")
	}
	return answer, chunks, nil
}

func systemPrompt(patchMode bool) string {
	if patchMode {
		return "Return only a raw unified diff. No prose. No markdown fences."
	}
	return "You are Adex, a local coding assistant. Answer from repo context. Cite exact file paths. If a file contains implementation, say it defines the feature. Do not invent missing files. Do not mention AGENTS.md unless user asks."
}

func buildPrompt(question string, pinned []RetrievedChunk, chunks []RetrievedChunk, graphContext string, sources []instructions.Source, patchMode bool) string {
	if patchMode {
		return buildPatchPrompt(question, pinned, chunks)
	}
	var out strings.Builder
	out.WriteString("# User task\n")
	out.WriteString(strings.TrimSpace(question))
	out.WriteString("\n\n# Required answer\n")
	if patchMode {
		out.WriteString("Return a raw unified diff only when enough context exists. Otherwise return a short blocker list. No markdown fence.\n")
	} else {
		out.WriteString("Answer directly in 5-8 concise bullets or short paragraphs. Mention relevant file paths. Do not answer with one word. If context is incomplete, say what is missing and still summarize known facts.\n")
	}
	if formattedInstructions := instructions.Format(sources); formattedInstructions != "" {
		out.WriteString("# Instructions\n")
		out.WriteString(formattedInstructions)
	}
	if len(pinned) > 0 {
		out.WriteString("\n\n# Pinned repo overview files\n")
		out.WriteString(formatChunks(pinned))
	}
	out.WriteString("\n\n# Retrieved repo context\n")
	out.WriteString(formatChunks(chunks))
	if graphContext != "" {
		out.WriteString("\n\n# Neo4j graph facts\n")
		out.WriteString(graphContext)
	}
	return out.String()
}

func buildPatchPrompt(question string, pinned []RetrievedChunk, chunks []RetrievedChunk) string {
	var out strings.Builder
	out.WriteString("Task:\n")
	out.WriteString(strings.TrimSpace(question))
	out.WriteString("\n\nReturn ONLY a unified diff patch that can be applied with `git apply`.")
	out.WriteString("\nNo explanation. No markdown fences.")
	out.WriteString("\nInside hunks, every line must start with exactly one of: space, +, or -.")
	out.WriteString("\nUse paths relative to repo root, for example:")
	out.WriteString("\n--- README.md\n+++ README.md\n@@ -1 +1 @@\n-old\n+new\n")
	out.WriteString("\nRelevant files:\n")
	out.WriteString(formatPatchFiles(compactPatchChunks(pinned, chunks)))
	return out.String()
}

func formatPatchFiles(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return "No relevant files found."
	}
	var out strings.Builder
	for _, chunk := range chunks {
		out.WriteString("\nFILE ")
		out.WriteString(chunk.Path)
		out.WriteString("\n<<<\n")
		out.WriteString(strings.TrimSpace(chunk.Content))
		out.WriteString("\n>>>\n")
	}
	return strings.TrimSpace(out.String())
}

func compactPatchChunks(pinned []RetrievedChunk, chunks []RetrievedChunk) []RetrievedChunk {
	seen := map[string]bool{}
	var out []RetrievedChunk
	for _, chunk := range append(pinned, chunks...) {
		if chunk.Path == "" || seen[chunk.Path] {
			continue
		}
		seen[chunk.Path] = true
		if len(chunk.Content) > 2200 {
			chunk.Content = chunk.Content[:2200]
		}
		out = append(out, chunk)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func (r *Runtime) graphContext(ctx context.Context, root string, chunks []RetrievedChunk) string {
	if r.graph == nil || len(chunks) == 0 {
		return ""
	}
	paths := uniqueChunkPaths(chunks)
	if len(paths) == 0 {
		return ""
	}
	graphCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err := r.graph.Query(graphCtx, `
MATCH (f:File {repo: $root})
WHERE f.path IN $paths
OPTIONAL MATCH (f)-[:DEFINES]->(s:Symbol)
OPTIONAL MATCH (f)-[:IMPORTS]->(p:Package)
RETURN f.path AS path,
       collect(DISTINCT s.kind + ':' + s.name)[0..20] AS symbols,
       collect(DISTINCT p.name)[0..20] AS imports
ORDER BY path
`, map[string]any{
		"root":  mustAbs(root),
		"paths": paths,
	})
	if err != nil || len(result.Rows) == 0 {
		return ""
	}
	var out strings.Builder
	for _, row := range result.Rows {
		if len(row) < 3 {
			continue
		}
		out.WriteString("- ")
		out.WriteString(fmt.Sprint(row[0]))
		out.WriteString(" defines=")
		out.WriteString(formatList(row[1]))
		out.WriteString(" imports=")
		out.WriteString(formatList(row[2]))
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

func uniqueChunkPaths(chunks []RetrievedChunk) []string {
	seen := map[string]bool{}
	var paths []string
	for _, chunk := range chunks {
		if chunk.Path == "" || seen[chunk.Path] {
			continue
		}
		seen[chunk.Path] = true
		paths = append(paths, chunk.Path)
	}
	return paths
}

func formatList(value any) string {
	values, ok := value.([]any)
	if !ok {
		return fmt.Sprint(value)
	}
	var out []string
	for _, item := range values {
		text := fmt.Sprint(item)
		if text != "" && text != "<nil>" {
			out = append(out, text)
		}
	}
	if len(out) == 0 {
		return "[]"
	}
	return "[" + strings.Join(out, ", ") + "]"
}

func buildRetryPrompt(question string, pinned []RetrievedChunk, chunks []RetrievedChunk) string {
	var out strings.Builder
	out.WriteString("Question: ")
	out.WriteString(strings.TrimSpace(question))
	out.WriteString("\n\nThe previous answer was unusable. Write a real answer now.\n")
	out.WriteString("Use this repo context:\n")
	for _, chunk := range append(pinned, chunks...) {
		out.WriteString("\nFile: ")
		out.WriteString(chunk.Path)
		out.WriteString("\n")
		out.WriteString(previewText(chunk.Content, 1200))
		out.WriteString("\n")
	}
	out.WriteString("\nAnswer with concrete bullets and file paths.")
	return out.String()
}

func loadPinnedContext(root string) []RetrievedChunk {
	files := []string{
		"README.md",
		"go.mod",
		"Makefile",
		"docker-compose.yml",
		filepath.Join("cmd", "adex", "main.go"),
		filepath.Join("internal", "graph", "indexer.go"),
		filepath.Join("internal", "graph", "neo4j.go"),
		filepath.Join("internal", "agent", "runtime.go"),
	}
	var chunks []RetrievedChunk
	for _, rel := range files {
		path := filepath.Join(root, rel)
		raw, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(raw)) == "" {
			continue
		}
		content := string(raw)
		if len(content) > 1500 {
			content = content[:1500]
		}
		chunks = append(chunks, RetrievedChunk{
			Path:       filepath.ToSlash(rel),
			Content:    content,
			Language:   strings.TrimPrefix(filepath.Ext(rel), "."),
			ChunkIndex: 0,
			ChunkTotal: 1,
			Score:      1,
		})
	}
	return chunks
}

func formatChunks(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return "No indexed context found."
	}
	var out strings.Builder
	for _, chunk := range chunks {
		if out.Len() >= MaxContextChars {
			out.WriteString("\n[context truncated]\n")
			break
		}
		out.WriteString("## ")
		out.WriteString(chunk.Path)
		out.WriteString(" score=")
		out.WriteString(fmt.Sprintf("%.4f", chunk.Score))
		out.WriteString(" chunk=")
		out.WriteString(strconv.Itoa(chunk.ChunkIndex + 1))
		out.WriteString("/")
		out.WriteString(strconv.Itoa(chunk.ChunkTotal))
		if len(chunk.Symbols) > 0 {
			out.WriteString(" symbols=")
			out.WriteString(strings.Join(limitStrings(chunk.Symbols, 12), ","))
		}
		out.WriteString("\n```")
		out.WriteString(chunk.Language)
		out.WriteString("\n")
		content := chunk.Content
		remaining := MaxContextChars - out.Len()
		if remaining < len(content) {
			content = content[:remaining]
		}
		out.WriteString(content)
		out.WriteString("\n```\n\n")
	}
	return strings.TrimSpace(out.String())
}

func embedText(chunk workspace.Chunk) string {
	var out strings.Builder
	out.WriteString("path: ")
	out.WriteString(chunk.Path)
	out.WriteString("\n")
	if len(chunk.Symbols) > 0 {
		out.WriteString("symbols: ")
		out.WriteString(strings.Join(limitStrings(chunk.Symbols, 40), ", "))
		out.WriteString("\n")
	}
	out.WriteString(chunk.Content)
	return out.String()
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func payloadString(payload map[string]any, key string) string {
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}

func payloadStrings(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func payloadInt(payload map[string]any, key string) int {
	raw, ok := payload[key]
	if !ok {
		return 0
	}
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		i, _ := strconv.Atoi(string(value))
		return i
	default:
		return 0
	}
}

type jsonNumber string

func stripPatchFences(answer string) string {
	answer = strings.TrimSpace(answer)
	if strings.HasPrefix(answer, "```") {
		if index := strings.Index(answer, "\n"); index >= 0 {
			answer = strings.TrimSpace(answer[index+1:])
		}
	}
	if strings.HasSuffix(answer, "```") {
		answer = strings.TrimSpace(strings.TrimSuffix(answer, "```"))
	}
	return answer
}

func isJunkAnswer(answer string, question string) bool {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return true
	}
	words := strings.Fields(trimmed)
	if len(words) <= 2 {
		return true
	}
	normalizedAnswer := strings.ToLower(strings.Trim(trimmed, ".!? "))
	normalizedQuestion := strings.ToLower(strings.TrimSpace(question))
	return normalizedAnswer == normalizedQuestion
}

func previewText(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "\n..."
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
