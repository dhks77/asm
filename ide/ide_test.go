package ide

import (
	"reflect"
	"runtime"
	"testing"
)

func TestOpenAppName(t *testing.T) {
	got, ok := openAppName([]string{"-a", "IntelliJ IDEA Ultimate"})
	if !ok {
		t.Fatal("expected app name to be parsed")
	}
	if got != "IntelliJ IDEA Ultimate" {
		t.Fatalf("openAppName() = %q, want %q", got, "IntelliJ IDEA Ultimate")
	}
}

func TestLaunchCommand_IntelliJOnDarwinUsesOpenWithArgs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific launcher behavior")
	}

	ide := IDE{
		Name:    "intellij",
		Command: "open",
		Args:    []string{"-a", "IntelliJ IDEA"},
	}

	command, args := ide.launchCommand("/tmp/project")
	if command != "open" {
		t.Fatalf("launchCommand() command = %q, want %q", command, "open")
	}
	want := []string{"-n", "-a", "IntelliJ IDEA", "--args", "/tmp/project"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("launchCommand() args = %#v, want %#v", args, want)
	}
}

func TestLaunchCommand_NonIntelliJAppendsPath(t *testing.T) {
	ide := IDE{
		Name:    "vscode",
		Command: "open",
		Args:    []string{"-a", "Visual Studio Code"},
	}

	command, args := ide.launchCommand("/tmp/project")
	if command != "open" {
		t.Fatalf("launchCommand() command = %q, want %q", command, "open")
	}
	want := []string{"-a", "Visual Studio Code", "/tmp/project"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("launchCommand() args = %#v, want %#v", args, want)
	}
}
