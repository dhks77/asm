package tracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nhn/asm/config"
)

func TestServiceResolvesTrackerPerPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	trackerPath := writeExecutableTracker(t, home, "demo.sh", `#!/bin/sh
cmd="$1"
shift
case "$cmd" in
  resolve)
    printf '{"name":"resolved:%s"}' "$1"
    ;;
  config-get)
    printf '{}'
    ;;
  *)
    exit 1
    ;;
esac
`)

	rootWithTracker := filepath.Join(t.TempDir(), "with-tracker")
	rootWithoutTracker := filepath.Join(t.TempDir(), "without-tracker")
	if err := os.MkdirAll(rootWithTracker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootWithoutTracker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveScope(&config.Config{DefaultTracker: filepath.Base(trackerPath)}, config.ScopeProject, rootWithTracker); err != nil {
		t.Fatal(err)
	}

	svc := NewService(time.Hour)
	if got := svc.Resolve(rootWithTracker, "feature/123").Name; got != "resolved:feature/123" {
		t.Fatalf("Resolve(with tracker) = %q, want %q", got, "resolved:feature/123")
	}
	if got := svc.Resolve(rootWithoutTracker, "feature/123").Name; got != "" {
		t.Fatalf("Resolve(without tracker) = %q, want empty", got)
	}
}

func TestServicePartitionsCacheByTrackerIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	firstPath := writeExecutableTracker(t, home, "first.sh", `#!/bin/sh
cmd="$1"
shift
case "$cmd" in
  resolve)
    printf '{"name":"first"}'
    ;;
  config-get)
    printf '{}'
    ;;
  *)
    exit 1
    ;;
esac
`)
	secondPath := writeExecutableTracker(t, home, "second.sh", `#!/bin/sh
cmd="$1"
shift
case "$cmd" in
  resolve)
    printf '{"name":"second"}'
    ;;
  config-get)
    printf '{}'
    ;;
  *)
    exit 1
    ;;
esac
`)

	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveScope(&config.Config{DefaultTracker: filepath.Base(firstPath)}, config.ScopeProject, root); err != nil {
		t.Fatal(err)
	}

	svc := NewService(time.Hour)
	if got := svc.Resolve(root, "feature/123").Name; got != "first" {
		t.Fatalf("Resolve(first tracker) = %q, want %q", got, "first")
	}

	if err := config.SaveScope(&config.Config{DefaultTracker: filepath.Base(secondPath)}, config.ScopeProject, root); err != nil {
		t.Fatal(err)
	}
	if got := svc.Resolve(root, "feature/123").Name; got != "second" {
		t.Fatalf("Resolve(second tracker) = %q, want %q", got, "second")
	}
}

func writeExecutableTracker(t *testing.T, home, name, content string) string {
	t.Helper()
	path := filepath.Join(home, ".asm", "trackers", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
