package graph

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryPostsCypher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/db/neo4j/tx/commit" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "neo4j" || pass != "pw" {
			t.Fatalf("bad auth user=%q pass=%q ok=%v", user, pass, ok)
		}
		var body struct {
			Statements []Statement `json:"statements"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Statements) != 1 || body.Statements[0].Statement != "RETURN 1 AS ok" {
			t.Fatalf("bad body: %+v", body)
		}
		w.Write([]byte(`{"results":[{"columns":["ok"],"data":[{"row":[1]}]}],"errors":[]}`))
	}))
	defer server.Close()

	client, err := New(server.URL, "neo4j", "pw")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(context.Background(), "RETURN 1 AS ok", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0][0].(float64) != 1 {
		t.Fatalf("bad result: %+v", result)
	}
}
