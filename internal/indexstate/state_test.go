package indexstate

import (
	"context"
	"testing"
)

func TestStoreReplaceAndApply(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Replace(ctx, []FileState{{Path: "a.go", Hash: "1"}, {Path: "b.go", Hash: "2"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(ctx, []FileState{{Path: "a.go", Hash: "3"}}, []string{"b.go"}); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 1 || state["a.go"] != "3" {
		t.Fatalf("state=%+v", state)
	}
}
