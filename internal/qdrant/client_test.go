package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureCollectionCreatesWhenMissing(t *testing.T) {
	var putSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/collections/code":
			http.NotFound(w, r)
		case r.Method == http.MethodPut && r.URL.Path == "/collections/code":
			putSeen = true
			var body struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Vectors.Size != 768 || body.Vectors.Distance != "Cosine" {
				t.Fatalf("unexpected vector config: %+v", body.Vectors)
			}
			w.Write([]byte(`{"result":true}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureCollection(context.Background(), "code", 768); err != nil {
		t.Fatal(err)
	}
	if !putSeen {
		t.Fatal("expected PUT collection request")
	}
}

func TestRecreateCollectionDeletesThenCreates(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodDelete:
			w.Write([]byte(`{"result":true}`))
		case http.MethodGet:
			http.NotFound(w, r)
		case http.MethodPut:
			w.Write([]byte(`{"result":true}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RecreateCollection(context.Background(), "code", 768); err != nil {
		t.Fatal(err)
	}
	want := []string{http.MethodDelete, http.MethodGet, http.MethodPut}
	if len(methods) != len(want) {
		t.Fatalf("methods=%v want=%v", methods, want)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods=%v want=%v", methods, want)
		}
	}
}

func TestUpsertAndSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/collections/code/points":
			if r.URL.Query().Get("wait") != "true" {
				t.Fatalf("missing wait=true")
			}
			var body struct {
				Points []Point `json:"points"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode upsert: %v", err)
			}
			if len(body.Points) != 1 || body.Points[0].Payload["path"] != "main.go" {
				t.Fatalf("unexpected points: %+v", body.Points)
			}
			w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/collections/code/points/search":
			w.Write([]byte(`{"result":[{"id":"1","score":0.9,"payload":{"path":"main.go","content":"package main"}}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Upsert(context.Background(), "code", []Point{{
		ID:      "11111111-1111-4111-8111-111111111111",
		Vector:  []float64{0.1, 0.2},
		Payload: map[string]any{"path": "main.go"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.Search(context.Background(), "code", []float64{0.1, 0.2}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Score != 0.9 {
		t.Fatalf("unexpected results: %+v", results)
	}
}
