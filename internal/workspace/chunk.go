package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	DefaultChunkBytes        = 3600
	DefaultChunkOverlapBytes = 600
)

type Chunk struct {
	ID         string
	Path       string
	Content    string
	Hash       string
	Language   string
	Symbols    []string
	Engine     string
	ChunkIndex int
	ChunkTotal int
}

func ChunkDocuments(docs []Document, maxBytes int, overlapBytes int) []Chunk {
	if maxBytes <= 0 {
		maxBytes = DefaultChunkBytes
	}
	if overlapBytes < 0 {
		overlapBytes = DefaultChunkOverlapBytes
	}
	if overlapBytes >= maxBytes {
		overlapBytes = maxBytes / 4
	}
	var chunks []Chunk
	for _, doc := range docs {
		parts := chunkText(doc.Content, maxBytes, overlapBytes)
		total := len(parts)
		for i, part := range parts {
			chunks = append(chunks, Chunk{
				ID:         stableUUID(doc.RelPath, doc.Hash, i),
				Path:       doc.RelPath,
				Content:    part,
				Hash:       doc.Hash,
				Language:   doc.Language,
				Symbols:    doc.Symbols,
				Engine:     doc.Engine,
				ChunkIndex: i,
				ChunkTotal: total,
			})
		}
	}
	return chunks
}

func chunkText(content string, maxBytes int, overlapBytes int) []string {
	lines := strings.SplitAfter(content, "\n")
	var chunks []string
	var current strings.Builder
	for _, line := range lines {
		if current.Len() > 0 && current.Len()+len(line) > maxBytes {
			previous := strings.TrimRight(current.String(), "\n")
			chunks = append(chunks, previous)
			current.Reset()
			current.WriteString(overlapSuffix(previous, overlapBytes))
			if current.Len() > 0 && !strings.HasSuffix(current.String(), "\n") {
				current.WriteString("\n")
			}
		}
		if len(line) > maxBytes {
			if current.Len() > 0 {
				chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}
			for len(line) > maxBytes {
				chunks = append(chunks, line[:maxBytes])
				line = line[maxBytes:]
			}
		}
		current.WriteString(line)
	}
	if strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
	}
	return chunks
}

func overlapSuffix(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	start := len(text) - maxBytes
	if index := strings.Index(text[start:], "\n"); index >= 0 && start+index+1 < len(text) {
		start += index + 1
	}
	return text[start:]
}

func stableUUID(path string, hash string, index int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d", path, hash, index)))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	hexed := hex.EncodeToString(bytes)
	return hexed[0:8] + "-" + hexed[8:12] + "-" + hexed[12:16] + "-" + hexed[16:20] + "-" + hexed[20:32]
}
