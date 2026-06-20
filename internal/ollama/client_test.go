package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatNonStreamJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %q, want /api/chat", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}

		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "gemma4:e2b-it-qat" {
			t.Fatalf("model = %q", request.Model)
		}
		if request.Stream {
			t.Fatal("stream = true, want false")
		}
		if len(request.Messages) != 1 || request.Messages[0].Content != "hello" {
			t.Fatalf("messages = %#v", request.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gemma4:e2b-it-qat","message":{"role":"assistant","content":"hi"},"done":true}`))
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Chat(context.Background(), "gemma4:e2b-it-qat", []Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if response.Message.Role != "assistant" || response.Message.Content != "hi" {
		t.Fatalf("message = %#v", response.Message)
	}
	if !response.Done {
		t.Fatal("done = false, want true")
	}
}

func TestEmbedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Fatalf("path = %q, want /api/embed", r.URL.Path)
		}

		var request EmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "embeddinggemma:latest" {
			t.Fatalf("model = %q", request.Model)
		}
		if len(request.Input) != 2 || request.Input[0] != "alpha" || request.Input[1] != "beta" {
			t.Fatalf("input = %#v", request.Input)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"embeddinggemma:latest","embeddings":[[0.1,0.2],[0.3,0.4]],"total_duration":10}`))
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Embed(context.Background(), "embeddinggemma:latest", []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(response.Embeddings) != 2 {
		t.Fatalf("embeddings len = %d, want 2", len(response.Embeddings))
	}
	if response.Embeddings[0][0] != 0.1 || response.Embeddings[1][1] != 0.4 {
		t.Fatalf("embeddings = %#v", response.Embeddings)
	}
}
