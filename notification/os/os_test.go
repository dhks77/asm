package osnotify

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nhn/asm/terminaldetect"
)

func TestSendUsesHelperWhenBundleIDPresent(t *testing.T) {
	prevRun := runCommand
	prevConfigDir := userConfigDir
	defer func() {
		runCommand = prevRun
		userConfigDir = prevConfigDir
	}()

	root := t.TempDir()
	userConfigDir = func() string { return root }
	spec := newHelperSpec(terminaldetect.App{Name: "iTerm", BundleID: "com.googlecode.iterm2"})
	if err := os.MkdirAll(filepath.Dir(spec.ExecutablePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(spec.InfoPlistPath, []byte(renderInfoPlist(spec)), 0o644); err != nil {
		t.Fatalf("WriteFile(info) error = %v", err)
	}
	if err := os.WriteFile(spec.ExecutablePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(executable) error = %v", err)
	}

	var gotName string
	var gotArgs []string
	runCommand = func(ctx context.Context, name string, args []string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	err := Send("ASM", "done", terminaldetect.Info{
		App: terminaldetect.App{Name: "iTerm", BundleID: "com.googlecode.iterm2"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotName != "open" {
		t.Fatalf("name = %q, want %q", gotName, "open")
	}
	want := []string{"-n", "-g", spec.AppPath, "--args", "ASM", "done"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
}

func TestSendBuildsHelperWhenMissing(t *testing.T) {
	prevRun := runCommand
	prevConfigDir := userConfigDir
	defer func() {
		runCommand = prevRun
		userConfigDir = prevConfigDir
	}()

	root := t.TempDir()
	userConfigDir = func() string { return root }

	type call struct {
		name string
		args []string
	}
	var calls []call
	runCommand = func(ctx context.Context, name string, args []string) error {
		calls = append(calls, call{name: name, args: append([]string(nil), args...)})
		if name == "xcrun" {
			for idx := 0; idx+1 < len(args); idx++ {
				if args[idx] == "-o" {
					if err := os.WriteFile(args[idx+1], []byte("#!/bin/sh\n"), 0o755); err != nil {
						return err
					}
					break
				}
			}
		}
		return nil
	}

	err := Send("ASM", "done", terminaldetect.Info{
		App: terminaldetect.App{Name: "Terminal", BundleID: "com.apple.Terminal"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if calls[0].name != "xcrun" {
		t.Fatalf("first call = %q, want %q", calls[0].name, "xcrun")
	}
	if calls[1].name != "codesign" {
		t.Fatalf("second call = %q, want %q", calls[1].name, "codesign")
	}
	if calls[2].name != "open" {
		t.Fatalf("third call = %q, want %q", calls[2].name, "open")
	}

	spec := newHelperSpec(terminaldetect.App{Name: "Terminal", BundleID: "com.apple.Terminal"})
	if _, err := os.Stat(spec.ExecutablePath); err != nil {
		t.Fatalf("Stat(executable) error = %v", err)
	}
	if got, err := os.ReadFile(spec.InfoPlistPath); err != nil {
		t.Fatalf("ReadFile(info) error = %v", err)
	} else if string(got) != renderInfoPlist(spec) {
		t.Fatalf("Info.plist did not match generated contents")
	}
}

func TestSendUsesGenericAppleScriptWithoutBundleID(t *testing.T) {
	prevRun := runCommand
	defer func() {
		runCommand = prevRun
	}()

	var gotName string
	var gotArgs []string
	runCommand = func(ctx context.Context, name string, args []string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	err := Send("ASM", "done", terminaldetect.Info{})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotName != "osascript" {
		t.Fatalf("name = %q, want %q", gotName, "osascript")
	}
	want := []string{"-e", `display notification "done" with title "ASM"`}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
}

func TestAppleScriptQuoteEscapesSpecialCharacters(t *testing.T) {
	got := appleScriptQuote(`a"b\c`)
	if got != `a\"b\\c` {
		t.Fatalf("appleScriptQuote() = %q, want %q", got, `a\"b\\c`)
	}
}
