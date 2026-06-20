package watcher

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Event struct {
	Path string
	Op   fsnotify.Op
}

type Options struct {
	Root     string
	Debounce time.Duration
	OnChange func(context.Context, []Event) error
}

func Run(ctx context.Context, options Options) error {
	if options.OnChange == nil {
		return fmt.Errorf("watch callback is required")
	}
	root, err := filepath.Abs(defaultRoot(options.Root))
	if err != nil {
		return fmt.Errorf("resolve watch root: %w", err)
	}
	debounce := options.Debounce
	if debounce <= 0 {
		debounce = 1200 * time.Millisecond
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()
	if err := addDirs(watcher, root); err != nil {
		return err
	}

	var pending []Event
	var timer *time.Timer
	var timerC <-chan time.Time
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		events := pending
		pending = nil
		return options.OnChange(ctx, events)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-watcher.Errors:
			return fmt.Errorf("watch error: %w", err)
		case event := <-watcher.Events:
			if shouldIgnore(event.Name) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = addDirs(watcher, event.Name)
				}
			}
			pending = append(pending, Event{Path: event.Name, Op: event.Op})
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(debounce)
			timerC = timer.C
		case <-timerC:
			timerC = nil
			if timer != nil {
				timer.Stop()
				timer = nil
			}
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

func addDirs(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if shouldSkipDir(entry.Name()) {
			return filepath.SkipDir
		}
		if err := watcher.Add(path); err != nil {
			return fmt.Errorf("watch %s: %w", path, err)
		}
		return nil
	})
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".vscode", ".adex", "node_modules", "vendor", "dist", "build", "coverage", ".next", ".nuxt", ".output", "tmp", "temp":
		return true
	default:
		return strings.HasPrefix(name, ".cache")
	}
}

func shouldIgnore(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") && base != ".env.example" && base != ".gitignore" {
		return true
	}
	return strings.Contains(path, string(filepath.Separator)+".adex"+string(filepath.Separator))
}

func defaultRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}
	return root
}
