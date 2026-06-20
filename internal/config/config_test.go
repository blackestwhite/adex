package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv(EnvOllamaURL, "")
	t.Setenv(EnvChatModel, "")
	t.Setenv(EnvEmbedModel, "")
	t.Setenv(EnvQdrantURL, "")
	t.Setenv(EnvCollection, "")
	t.Setenv(EnvNeo4jURL, "")
	t.Setenv(EnvNeo4jUser, "")
	t.Setenv(EnvNeo4jPass, "")

	cfg := Load()
	if cfg.OllamaURL != DefaultOllamaURL {
		t.Fatalf("OllamaURL = %q, want %q", cfg.OllamaURL, DefaultOllamaURL)
	}
	if cfg.ChatModel != DefaultChatModel {
		t.Fatalf("ChatModel = %q, want %q", cfg.ChatModel, DefaultChatModel)
	}
	if cfg.EmbedModel != DefaultEmbedModel {
		t.Fatalf("EmbedModel = %q, want %q", cfg.EmbedModel, DefaultEmbedModel)
	}
	if cfg.QdrantURL != DefaultQdrantURL {
		t.Fatalf("QdrantURL = %q, want %q", cfg.QdrantURL, DefaultQdrantURL)
	}
	if cfg.Collection != DefaultCollection {
		t.Fatalf("Collection = %q, want %q", cfg.Collection, DefaultCollection)
	}
	if cfg.Neo4jURL != DefaultNeo4jURL {
		t.Fatalf("Neo4jURL = %q, want %q", cfg.Neo4jURL, DefaultNeo4jURL)
	}
	if cfg.Neo4jUser != DefaultNeo4jUser {
		t.Fatalf("Neo4jUser = %q, want %q", cfg.Neo4jUser, DefaultNeo4jUser)
	}
	if cfg.Neo4jPass != DefaultNeo4jPass {
		t.Fatalf("Neo4jPass = %q, want %q", cfg.Neo4jPass, DefaultNeo4jPass)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv(EnvOllamaURL, "http://ollama.test")
	t.Setenv(EnvChatModel, "chat-model")
	t.Setenv(EnvEmbedModel, "embed-model")
	t.Setenv(EnvQdrantURL, "http://qdrant.test")
	t.Setenv(EnvCollection, "code")
	t.Setenv(EnvNeo4jURL, "http://neo4j.test")
	t.Setenv(EnvNeo4jUser, "neo")
	t.Setenv(EnvNeo4jPass, "secret")

	cfg := Load()
	if cfg.OllamaURL != "http://ollama.test" {
		t.Fatalf("OllamaURL = %q", cfg.OllamaURL)
	}
	if cfg.ChatModel != "chat-model" {
		t.Fatalf("ChatModel = %q", cfg.ChatModel)
	}
	if cfg.EmbedModel != "embed-model" {
		t.Fatalf("EmbedModel = %q", cfg.EmbedModel)
	}
	if cfg.QdrantURL != "http://qdrant.test" {
		t.Fatalf("QdrantURL = %q", cfg.QdrantURL)
	}
	if cfg.Collection != "code" {
		t.Fatalf("Collection = %q", cfg.Collection)
	}
	if cfg.Neo4jURL != "http://neo4j.test" {
		t.Fatalf("Neo4jURL = %q", cfg.Neo4jURL)
	}
	if cfg.Neo4jUser != "neo" {
		t.Fatalf("Neo4jUser = %q", cfg.Neo4jUser)
	}
	if cfg.Neo4jPass != "secret" {
		t.Fatalf("Neo4jPass = %q", cfg.Neo4jPass)
	}
}
