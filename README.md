# ade-x

Local Go coding assistant using Ollama + Qdrant.

Models expected:

- chat: `gemma4:e2b-it-qat`
- embeddings: `embeddinggemma:latest`

## Quickstart

```sh
docker compose up -d qdrant
docker compose --profile graph up -d neo4j
go test ./...
go run ./cmd/adex doctor
go run ./cmd/adex graph-doctor
go run ./cmd/adex index -root . -full -chunk-overlap-bytes 600
go run ./cmd/adex watch-index -root .
go run ./cmd/adex graph-index -root .
go run ./cmd/adex search "where is qdrant used?"
go run ./cmd/adex ask "explain the indexing flow"
go run ./cmd/adex ask --sources -k 2 "explain the indexing flow"
go run ./cmd/adex patch "add a test for empty search query"
go run ./cmd/adex fix "add a test for empty search query"
```

## Commands

- `doctor`: checks Ollama chat, Ollama embeddings, and Qdrant.
- `index`: incrementally scans repo text/code files, chunks them with overlap, embeds changed files with Ollama, deletes stale vectors, and stores hashes in `.adex/index.sqlite`. Add `-full` to rebuild the collection.
- `watch-index`: watches repo files and runs incremental indexing after a debounce.
- `search`: semantic code search over indexed chunks.
- `ask`: retrieves repo context and asks the local chat model. Add `--sources` to print retrieved chunks.
- `patch`: retrieves repo context and asks for a unified diff.
- `fix`: generates a patch, validates it with `git apply --check`, applies it, runs a verifier command, and retries after verifier failures with failure output.
- `graph-doctor`: checks Neo4j.
- `graph-index`: scans the repo and stores `Repo`, `File`, `Symbol`, and `Package` nodes in Neo4j.

## Config

```sh
OLLAMA_URL=http://localhost:11434
ADEX_CHAT_MODEL=gemma4:e2b-it-qat
ADEX_EMBED_MODEL=embeddinggemma:latest
QDRANT_URL=http://localhost:6333
ADEX_COLLECTION=adex_code
NEO4J_URL=http://localhost:7474
NEO4J_USER=neo4j
NEO4J_PASSWORD=adex-local
```

## Design

Codex-inspired loop:

```text
prompt -> load AGENTS.md -> retrieve Qdrant context -> model -> answer/patch -> verify
```

V1 uses Qdrant for semantic repo memory and Neo4j for explicit code graph facts.

```sh
docker compose --profile graph up -d
go run ./cmd/adex graph-index -root .
```

Current graph shape: `Repo -> CONTAINS -> File`, `File -> DEFINES -> Symbol`, `File -> IMPORTS -> Package`.

Code intelligence uses Tree-sitter for Go and JavaScript/TypeScript symbols, with Go parser fallback for imports/symbols. Incremental indexing uses SQLite file hashes plus Qdrant path deletes/upserts.
