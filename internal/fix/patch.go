package fix

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ade-x/internal/agent"
)

const (
	DefaultAttempts      = 3
	DefaultVerifier      = "go test ./..."
	DefaultCommandTimout = 5 * time.Minute
)

type Runner struct {
	Runtime  *agent.Runtime
	Root     string
	TopK     int
	Verify   string
	Attempts int
	Timeout  time.Duration
}

type Result struct {
	Attempts     int
	ChangedFiles []string
	Verifier     string
	VerifyOutput string
	Patch        string
}

func (r Runner) Run(ctx context.Context, task string) (Result, error) {
	root, err := filepath.Abs(defaultString(r.Root, "."))
	if err != nil {
		return Result{}, fmt.Errorf("resolve root: %w", err)
	}
	if r.Runtime == nil {
		return Result{}, fmt.Errorf("runtime is required")
	}
	attempts := r.Attempts
	if attempts <= 0 {
		attempts = DefaultAttempts
	}
	verify := defaultString(r.Verify, DefaultVerifier)
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultCommandTimout
	}

	var lastErr error
	var repairContext string
	for attempt := 1; attempt <= attempts; attempt++ {
		prompt := buildPatchTask(task, repairContext)
		answer, _, err := r.Runtime.Ask(ctx, root, prompt, r.TopK, true)
		if err != nil {
			lastErr = err
			repairContext = "Previous patch generation failed:\n" + err.Error()
			continue
		}
		patch, err := ExtractUnifiedDiff(answer)
		if err != nil {
			lastErr = fmt.Errorf("%w; answer: %s", err, excerpt(answer, 800))
			repairContext = "Previous answer was not a valid unified diff:\n" + answer
			continue
		}
		files := ChangedFiles(patch)
		if err := gitApply(ctx, root, patch, true, false, timeout); err != nil {
			lastErr = err
			repairContext = fmt.Sprintf("Previous patch failed `git apply --check`:\n%s\n\nPatch:\n%s", err.Error(), patch)
			continue
		}
		if err := gitApply(ctx, root, patch, false, false, timeout); err != nil {
			lastErr = err
			repairContext = fmt.Sprintf("Previous patch passed check but failed apply:\n%s\n\nPatch:\n%s", err.Error(), patch)
			continue
		}

		output, err := runShell(ctx, root, verify, timeout)
		if err == nil {
			return Result{
				Attempts:     attempt,
				ChangedFiles: files,
				Verifier:     verify,
				VerifyOutput: output,
				Patch:        patch,
			}, nil
		}

		lastErr = err
		revertErr := gitApply(ctx, root, patch, false, true, timeout)
		if revertErr != nil {
			return Result{Attempts: attempt, ChangedFiles: files, Verifier: verify, VerifyOutput: output, Patch: patch},
				fmt.Errorf("verification failed and patch could not be reverted: verify: %w; revert: %v", err, revertErr)
		}
		repairContext = fmt.Sprintf("Previous patch was reverted because verifier failed.\nVerifier: %s\nOutput:\n%s\nError: %s\n\nFailed patch:\n%s", verify, output, err.Error(), patch)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no patch generated")
	}
	return Result{Attempts: attempts, Verifier: verify}, fmt.Errorf("fix failed after %d attempts: %w", attempts, lastErr)
}

func ExtractUnifiedDiff(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty patch")
	}
	lines := strings.Split(text, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no unified diff header found")
	}
	var patchLines []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			break
		}
		patchLines = append(patchLines, normalizePatchLine(line))
	}
	patch := strings.TrimSpace(strings.Join(patchLines, "\n")) + "\n"
	if !strings.Contains(patch, "\n+++ ") || !strings.Contains(patch, "\n@@") {
		return "", fmt.Errorf("unified diff missing file or hunk header")
	}
	if !hasHunkEdit(patch, '+') || !hasHunkEdit(patch, '-') {
		return "", fmt.Errorf("unified diff must include both removed and added hunk lines")
	}
	return patch, nil
}

func hasHunkEdit(patch string, prefix byte) bool {
	scanner := bufio.NewScanner(strings.NewReader(patch))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 || line[0] != prefix {
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		return true
	}
	return false
}

func normalizePatchLine(line string) string {
	line = strings.ReplaceAll(line, "\\`", "`")
	if strings.HasPrefix(line, "- - ") {
		return "-- " + strings.TrimPrefix(line, "- - ")
	}
	if strings.HasPrefix(line, "+ - ") {
		return "+- " + strings.TrimPrefix(line, "+ - ")
	}
	return line
}

func ChangedFiles(patch string) []string {
	seen := map[string]bool{}
	var files []string
	scanner := bufio.NewScanner(strings.NewReader(patch))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "+++ ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
		if path == "/dev/null" {
			continue
		}
		path = strings.TrimPrefix(path, "b/")
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	return files
}

func buildPatchTask(task string, repairContext string) string {
	var out strings.Builder
	out.WriteString(task)
	out.WriteString("\n\nReturn a raw unified diff patch only. Do not include prose.")
	if strings.TrimSpace(repairContext) != "" {
		out.WriteString("\n\nRepair context:\n")
		out.WriteString(repairContext)
	}
	return out.String()
}

func gitApply(ctx context.Context, root string, patch string, check bool, reverse bool, timeout time.Duration) error {
	args := []string{"apply"}
	if check {
		args = append(args, "--check")
	}
	args = append(args, "--recount")
	if reverse {
		args = append(args, "-R")
	}
	args = append(args, "-")

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(patch)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(output.String()))
	}
	return nil
}

func runShell(ctx context.Context, root string, command string, timeout time.Duration) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "bash", "-lc", command)
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	text := strings.TrimSpace(output.String())
	if err != nil {
		return text, fmt.Errorf("%s: %w", command, err)
	}
	return text, nil
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func excerpt(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max]) + "..."
}
