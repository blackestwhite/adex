package config

import "os"

const (
	DefaultOllamaURL  = "http://localhost:11434"
	DefaultChatModel  = "gemma4:e2b-it-qat"
	DefaultEmbedModel = "embeddinggemma:latest"
	DefaultQdrantURL  = "http://localhost:6333"
	DefaultCollection = "adex_code"
	DefaultNeo4jURL   = "http://localhost:7474"
	DefaultNeo4jUser  = "neo4j"
	DefaultNeo4jPass  = "adex-local"
	EnvOllamaURL      = "OLLAMA_URL"
	EnvChatModel      = "ADEX_CHAT_MODEL"
	EnvEmbedModel     = "ADEX_EMBED_MODEL"
	EnvQdrantURL      = "QDRANT_URL"
	EnvCollection     = "ADEX_COLLECTION"
	EnvNeo4jURL       = "NEO4J_URL"
	EnvNeo4jUser      = "NEO4J_USER"
	EnvNeo4jPass      = "NEO4J_PASSWORD"
)

type Config struct {
	OllamaURL  string
	ChatModel  string
	EmbedModel string
	QdrantURL  string
	Collection string
	Neo4jURL   string
	Neo4jUser  string
	Neo4jPass  string
}

func Load() Config {
	return Config{
		OllamaURL:  envOrDefault(EnvOllamaURL, DefaultOllamaURL),
		ChatModel:  envOrDefault(EnvChatModel, DefaultChatModel),
		EmbedModel: envOrDefault(EnvEmbedModel, DefaultEmbedModel),
		QdrantURL:  envOrDefault(EnvQdrantURL, DefaultQdrantURL),
		Collection: envOrDefault(EnvCollection, DefaultCollection),
		Neo4jURL:   envOrDefault(EnvNeo4jURL, DefaultNeo4jURL),
		Neo4jUser:  envOrDefault(EnvNeo4jUser, DefaultNeo4jUser),
		Neo4jPass:  envOrDefault(EnvNeo4jPass, DefaultNeo4jPass),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
