package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"ade-x/internal/agent"
	"ade-x/internal/config"
	"ade-x/internal/fix"
	"ade-x/internal/graph"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "doctor":
		return runDoctor(args[1:])
	case "index":
		return runIndex(args[1:])
	case "search":
		return runSearch(args[1:])
	case "ask":
		return runAsk(args[1:], false)
	case "patch":
		return runAsk(args[1:], true)
	case "fix":
		return runFix(args[1:])
	case "graph-doctor":
		return runGraphDoctor(args[1:])
	case "graph-index":
		return runGraphIndex(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runFix(args []string) error {
	set := flag.NewFlagSet("fix", flag.ContinueOnError)
	root := set.String("root", ".", "workspace root")
	topK := set.Int("k", agent.DefaultAskTopK, "context chunk count")
	verify := set.String("verify", fix.DefaultVerifier, "verification command")
	attempts := set.Int("attempts", fix.DefaultAttempts, "max patch attempts")
	timeout := set.Duration("timeout", 15*time.Minute, "deadline")
	commandTimeout := set.Duration("command-timeout", fix.DefaultCommandTimout, "per command deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	task := strings.TrimSpace(strings.Join(set.Args(), " "))
	if task == "" {
		return fmt.Errorf("fix task is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	rt, err := newRuntime()
	if err != nil {
		return err
	}
	result, err := fix.Runner{
		Runtime:  rt,
		Root:     *root,
		TopK:     *topK,
		Verify:   *verify,
		Attempts: *attempts,
		Timeout:  *commandTimeout,
	}.Run(ctx, task)
	if err != nil {
		return err
	}
	fmt.Printf("fixed attempts=%d verifier=%q\n", result.Attempts, result.Verifier)
	if len(result.ChangedFiles) > 0 {
		fmt.Println("changed:")
		for _, file := range result.ChangedFiles {
			fmt.Println("- " + file)
		}
	}
	if strings.TrimSpace(result.VerifyOutput) != "" {
		fmt.Println("\nverify output:")
		fmt.Println(result.VerifyOutput)
	}
	return nil
}

func runGraphDoctor(args []string) error {
	set := flag.NewFlagSet("graph-doctor", flag.ContinueOnError)
	timeout := set.Duration("timeout", 90*time.Second, "deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := newGraphClient()
	if err != nil {
		return err
	}
	if err := client.Health(ctx); err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}

func runGraphIndex(args []string) error {
	set := flag.NewFlagSet("graph-index", flag.ContinueOnError)
	root := set.String("root", ".", "workspace root")
	maxFileBytes := set.Int64("max-file-bytes", 512*1024, "max file bytes")
	timeout := set.Duration("timeout", 5*time.Minute, "deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client, err := newGraphClient()
	if err != nil {
		return err
	}
	stats, err := graph.IndexWorkspace(ctx, client, *root, *maxFileBytes)
	if err != nil {
		return err
	}
	fmt.Printf("graph-indexed root=%s files=%d symbols=%d imports=%d statements=%d\n",
		stats.Root, stats.Files, stats.Symbols, stats.Imports, stats.Statements)
	return nil
}

func runDoctor(args []string) error {
	set := flag.NewFlagSet("doctor", flag.ContinueOnError)
	timeout := set.Duration("timeout", 90*time.Second, "deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	rt, err := newRuntime()
	if err != nil {
		return err
	}
	if err := rt.Doctor(ctx); err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}

func runIndex(args []string) error {
	set := flag.NewFlagSet("index", flag.ContinueOnError)
	root := set.String("root", ".", "workspace root")
	maxFileBytes := set.Int64("max-file-bytes", 512*1024, "max file bytes")
	chunkBytes := set.Int("chunk-bytes", 3600, "chunk bytes")
	chunkOverlapBytes := set.Int("chunk-overlap-bytes", 600, "chunk overlap bytes")
	batchSize := set.Int("batch", agent.DefaultBatchSize, "embedding batch size")
	timeout := set.Duration("timeout", 30*time.Minute, "deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	rt, err := newRuntime()
	if err != nil {
		return err
	}
	stats, err := rt.Index(ctx, *root, *maxFileBytes, *chunkBytes, *chunkOverlapBytes, *batchSize)
	if err != nil {
		return err
	}
	fmt.Printf("indexed root=%s docs=%d chunks=%d embeddings=%d vector=%d collection=%s\n",
		stats.Root, stats.Documents, stats.Chunks, stats.Embeddings, stats.VectorSize, stats.Collection)
	return nil
}

func runSearch(args []string) error {
	set := flag.NewFlagSet("search", flag.ContinueOnError)
	topK := set.Int("k", agent.DefaultTopK, "result count")
	timeout := set.Duration("timeout", 2*time.Minute, "deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(set.Args(), " "))
	if query == "" {
		return fmt.Errorf("search query is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	rt, err := newRuntime()
	if err != nil {
		return err
	}
	results, err := rt.Search(ctx, query, *topK)
	if err != nil {
		return err
	}
	for i, result := range results {
		fmt.Printf("[%d] %.4f %s chunk=%d/%d\n", i+1, result.Score, result.Path, result.ChunkIndex+1, result.ChunkTotal)
		if len(result.Symbols) > 0 {
			fmt.Printf("symbols: %s\n", strings.Join(limit(result.Symbols, 12), ", "))
		}
		fmt.Println(preview(result.Content, 700))
		fmt.Println()
	}
	return nil
}

func runAsk(args []string, patchMode bool) error {
	name := "ask"
	if patchMode {
		name = "patch"
	}
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	root := set.String("root", ".", "workspace root")
	topK := set.Int("k", agent.DefaultAskTopK, "context chunk count")
	showSources := set.Bool("sources", false, "print retrieved sources to stderr")
	timeout := set.Duration("timeout", 5*time.Minute, "deadline")
	if err := set.Parse(args); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(set.Args(), " "))
	if question == "" {
		return fmt.Errorf("%s prompt is required", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	rt, err := newRuntime()
	if err != nil {
		return err
	}
	answer, sources, err := rt.Ask(ctx, *root, question, *topK, patchMode)
	if err != nil {
		return err
	}
	if patchMode {
		answer, err = fix.ExtractUnifiedDiff(answer)
		if err != nil {
			return err
		}
	}
	fmt.Println(answer)
	if *showSources && len(sources) > 0 {
		fmt.Fprintln(os.Stderr, "\nsources:")
		for _, source := range sources {
			fmt.Fprintf(os.Stderr, "- %.4f %s chunk=%d/%d\n", source.Score, source.Path, source.ChunkIndex+1, source.ChunkTotal)
		}
	}
	return nil
}

func newRuntime() (*agent.Runtime, error) {
	return agent.NewRuntime(config.Load())
}

func newGraphClient() (*graph.Client, error) {
	cfg := config.Load()
	return graph.New(cfg.Neo4jURL, cfg.Neo4jUser, cfg.Neo4jPass)
}

func usage() {
	fmt.Print(`adex - local Ollama coding assistant

commands:
  doctor                         check Ollama + Qdrant
  index  [-root .]               embed repo chunks into Qdrant
  search [-k 8] <query>          semantic repo search
  ask    [-root .] [-k 2] <q>    RAG answer
  patch  [-root .] [-k 2] <task> ask model for unified diff
  fix    [-root .] <task>        apply patch and run verifier
  graph-doctor                   check Neo4j
  graph-index [-root .]          write code graph to Neo4j

env:
  OLLAMA_URL=http://localhost:11434
  ADEX_CHAT_MODEL=gemma4:e2b-it-qat
  ADEX_EMBED_MODEL=embeddinggemma:latest
  QDRANT_URL=http://localhost:6333
  ADEX_COLLECTION=adex_code
  NEO4J_URL=http://localhost:7474
  NEO4J_USER=neo4j
  NEO4J_PASSWORD=adex-local
`)
}

func preview(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "\n..."
}

func limit(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return values[:max]
}
