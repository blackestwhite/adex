package agent

import "testing"

func TestStripPatchFences(t *testing.T) {
	got := stripPatchFences("```diff\n--- a\n+++ b\n@@\n-x\n+y\n```")
	want := "--- a\n+++ b\n@@\n-x\n+y"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestIsJunkAnswer(t *testing.T) {
	tests := []struct {
		answer string
		want   bool
	}{
		{"", true},
		{"This", true},
		{"explain this repo", true},
		{"This repo is a Go CLI that indexes code into Qdrant.", false},
	}
	for _, test := range tests {
		if got := isJunkAnswer(test.answer, "explain this repo"); got != test.want {
			t.Fatalf("isJunkAnswer(%q)=%v want %v", test.answer, got, test.want)
		}
	}
}
