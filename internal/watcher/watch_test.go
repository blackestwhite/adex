package watcher

import "testing"

func TestShouldIgnoreStateAndHiddenFiles(t *testing.T) {
	if !shouldIgnore("/repo/.adex/index.sqlite") {
		t.Fatal("expected .adex state to be ignored")
	}
	if !shouldIgnore("/repo/.DS_Store") {
		t.Fatal("expected hidden file to be ignored")
	}
	if shouldIgnore("/repo/.gitignore") {
		t.Fatal("expected .gitignore to be watchable")
	}
}
