package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const notificationTimeout = 5 * time.Second
const trashFallbackTimeout = 5 * time.Second

// IDEEntry describes one built-in IDE launcher for a platform.
type IDEEntry struct {
	Name    string
	Command string
	Args    []string
}

// Platform abstracts host-OS and environment behavior. Adding a new OS should
// require only a new implementation that satisfies this interface.
type Platform interface {
	Name() string
	HomeDir() (string, error)
	WorkingDir() (string, error)
	TempDir() string
	ExecutablePath() (string, error)
	UserConfigDir() string
	Notify(title, body string)
	MoveToTrash(path string) error
	OpenURL(url string) error
	RevealPath(path string) error
	BuiltinIDEs() []IDEEntry
	PrepareIDEOpen(name, command string, args []string, path string) (string, []string)
}

// Registry stores named platform implementations.
type Registry struct {
	platforms   map[string]Platform
	defaultName string
	order       []string
}

// NewRegistry creates an empty platform registry.
func NewRegistry() *Registry {
	return &Registry{platforms: make(map[string]Platform)}
}

// Register adds a platform implementation to the registry.
func (r *Registry) Register(p Platform) {
	name := strings.TrimSpace(p.Name())
	if name == "" {
		return
	}
	if _, exists := r.platforms[name]; !exists {
		r.order = append(r.order, name)
	}
	r.platforms[name] = p
}

// SetDefault selects the fallback platform by name.
func (r *Registry) SetDefault(name string) {
	if _, ok := r.platforms[name]; ok {
		r.defaultName = name
	}
}

// Get returns a platform by name.
func (r *Registry) Get(name string) Platform {
	return r.platforms[name]
}

// Default returns the configured fallback platform.
func (r *Registry) Default() Platform {
	if p, ok := r.platforms[r.defaultName]; ok {
		return p
	}
	if len(r.order) > 0 {
		return r.platforms[r.order[0]]
	}
	return nil
}

var (
	defaultRegistry = newBuiltinRegistry()
	currentMu       sync.RWMutex
	currentOverride Platform
)

// Current returns the active platform implementation for the current OS, with
// a test override when one has been installed.
func Current() Platform {
	currentMu.RLock()
	override := currentOverride
	currentMu.RUnlock()
	if override != nil {
		return override
	}
	if p := defaultRegistry.Get(runtime.GOOS); p != nil {
		return p
	}
	return defaultRegistry.Default()
}

// SetCurrentForTesting installs a temporary override and returns a restore
// closure for callers to defer.
func SetCurrentForTesting(p Platform) func() {
	currentMu.Lock()
	prev := currentOverride
	currentOverride = p
	currentMu.Unlock()
	return func() {
		currentMu.Lock()
		currentOverride = prev
		currentMu.Unlock()
	}
}

type platformImpl struct {
	name           string
	homeDir        func() (string, error)
	workingDir     func() (string, error)
	tempDir        func() string
	executablePath func() (string, error)
	userConfigDir  func() string
	notify         func(title, body string)
	moveToTrash    func(path string) error
	openURL        func(url string) error
	revealPath     func(path string) error
	builtinIDEs    []IDEEntry
	prepareIDEOpen func(name, command string, args []string, path string) (string, []string)
}

func (p *platformImpl) Name() string { return p.name }

func (p *platformImpl) HomeDir() (string, error) {
	if p.homeDir != nil {
		return p.homeDir()
	}
	return os.UserHomeDir()
}

func (p *platformImpl) WorkingDir() (string, error) {
	if p.workingDir != nil {
		return p.workingDir()
	}
	return os.Getwd()
}

func (p *platformImpl) TempDir() string {
	if p.tempDir != nil {
		return p.tempDir()
	}
	return os.TempDir()
}

func (p *platformImpl) ExecutablePath() (string, error) {
	if p.executablePath != nil {
		return p.executablePath()
	}
	return os.Executable()
}

func (p *platformImpl) UserConfigDir() string {
	if p.userConfigDir != nil {
		return p.userConfigDir()
	}
	return defaultUserConfigDir(p)
}

func (p *platformImpl) Notify(title, body string) {
	if p.notify != nil {
		p.notify(title, body)
	}
}

func (p *platformImpl) MoveToTrash(path string) error {
	if p.moveToTrash == nil {
		return fmt.Errorf("platform %q does not support trash", p.name)
	}
	return p.moveToTrash(path)
}

func (p *platformImpl) OpenURL(url string) error {
	if p.openURL == nil {
		return fmt.Errorf("platform %q does not support url open", p.name)
	}
	return p.openURL(url)
}

func (p *platformImpl) RevealPath(path string) error {
	if p.revealPath == nil {
		return fmt.Errorf("platform %q does not support path reveal", p.name)
	}
	return p.revealPath(path)
}

func (p *platformImpl) BuiltinIDEs() []IDEEntry {
	out := make([]IDEEntry, len(p.builtinIDEs))
	for i, entry := range p.builtinIDEs {
		out[i] = IDEEntry{Name: entry.Name, Command: entry.Command, Args: append([]string(nil), entry.Args...)}
	}
	return out
}

func (p *platformImpl) PrepareIDEOpen(name, command string, args []string, path string) (string, []string) {
	if p.prepareIDEOpen == nil {
		return appendPath(command, args, path)
	}
	return p.prepareIDEOpen(name, command, args, path)
}

func newBuiltinRegistry() *Registry {
	reg := NewRegistry()
	for _, p := range []Platform{
		newDarwinPlatform(),
		newUnsupportedPlatform(),
	} {
		reg.Register(p)
	}
	reg.SetDefault("unsupported")
	return reg
}

func defaultUserConfigDir(p Platform) string {
	home, err := p.HomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".asm")
	}
	return filepath.Join(p.TempDir(), ".asm")
}

func appendPath(command string, args []string, path string) (string, []string) {
	out := append([]string(nil), args...)
	out = append(out, path)
	return command, out
}

func appendPathWithName(_ string, command string, args []string, path string) (string, []string) {
	return appendPath(command, args, path)
}

func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Start()
}

func runBestEffort(timeout time.Duration, name string, args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = exec.CommandContext(ctx, name, args...).Run()
}

func moveByRename(path, trashDir string, fallback func(string) error) error {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return err
	}

	dst, err := uniqueDestination(trashDir, filepath.Base(clean))
	if err != nil {
		return err
	}
	if err := os.Rename(clean, dst); err != nil {
		if fallback != nil {
			if fallbackErr := fallback(clean); fallbackErr == nil {
				return nil
			} else {
				return fmt.Errorf("rename to trash failed: %w (fallback: %v)", err, fallbackErr)
			}
		}
		return err
	}
	return nil
}

func uniqueDestination(dir, name string) (string, error) {
	base, ext := splitName(name)
	candidate := filepath.Join(dir, name)
	if _, err := os.Lstat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil {
		return "", err
	}

	for i := 2; i < 10_000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s %d%s", base, i, ext))
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate trash destination for %q", name)
}

func splitName(name string) (string, string) {
	ext := filepath.Ext(name)
	if ext == "" {
		return name, ""
	}
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		return name, ""
	}
	return base, ext
}

func moveDarwinWithFinder(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), trashFallbackTimeout)
	defer cancel()

	script := `tell application "Finder" to delete POSIX file "` + escapeAppleScript(path) + `"`
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func openAppName(args []string) (string, bool) {
	for idx := 0; idx+1 < len(args); idx++ {
		if args[idx] == "-a" {
			name := strings.TrimSpace(args[idx+1])
			if name != "" {
				return name, true
			}
			return "", false
		}
	}
	return "", false
}

func escapeAppleScript(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// parseClientActivity is kept small and exported only to tests via the
// platform behavior above; strconv usage stays local here.
func parseClientActivity(raw string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	return v
}
