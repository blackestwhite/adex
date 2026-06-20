package fix

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractUnifiedDiff(t *testing.T) {
	got, err := ExtractUnifiedDiff("here\n```diff\n--- a.txt\n+++ a.txt\n@@ -1 +1 @@\n-a\n+b\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "--- a.txt\n+++ a.txt\n@@") {
		t.Fatalf("bad patch: %q", got)
	}
}

func TestExtractUnifiedDiffRejectsProse(t *testing.T) {
	if _, err := ExtractUnifiedDiff("not a patch"); err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractUnifiedDiffRejectsOneSidedPatch(t *testing.T) {
	if _, err := ExtractUnifiedDiff("--- a.txt\n+++ a.txt\n@@ -1 +1 @@\n-a\n"); err == nil {
		t.Fatal("expected error")
	}
}

func TestChangedFiles(t *testing.T) {
	files := ChangedFiles("diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+x\n")
	want := []string{"a.txt", "new.txt"}
	if len(files) != len(want) {
		t.Fatalf("files=%v want=%v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files=%v want=%v", files, want)
		}
	}
}

func TestExtractUnifiedDiffNormalizesMarkdownBulletEscapes(t *testing.T) {
	got, err := ExtractUnifiedDiff("--- README.md\n+++ README.md\n@@ -1 +1 @@\n- - `fix`: old\n+ - \\`fix\\`: new\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "\\`") {
		t.Fatalf("backticks not normalized: %q", got)
	}
	if !strings.Contains(got, "-- `fix`: old") || !strings.Contains(got, "+- `fix`: new") {
		t.Fatalf("markdown bullets not normalized: %q", got)
	}
}

func TestGitApplyAndReverse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := "--- a.txt\n+++ a.txt\n@@ -1 +1 @@\n-a\n+b\n"
	if err := gitApply(context.Background(), dir, patch, true, false, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := gitApply(context.Background(), dir, patch, false, false, time.Second); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "b\n" {
		t.Fatalf("after apply = %q", raw)
	}
	if err := gitApply(context.Background(), dir, patch, false, true, time.Second); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "a\n" {
		t.Fatalf("after reverse = %q", raw)
	}
}
