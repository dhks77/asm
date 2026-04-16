package asmlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nhn/asm/config"
)

var (
	mu      sync.Mutex
	logFile *os.File
)

// Path returns the shared asm log file path.
func Path() string {
	return filepath.Join(config.UserConfigDir(), "logs", "asm.log")
}

// Init opens the shared log file and writes a process start marker.
func Init() {
	mu.Lock()
	defer mu.Unlock()

	if !ensureOpenLocked() {
		return
	}
	fmt.Fprintf(logFile, "\n=== asm started at %s pid=%d ===\n",
		time.Now().Format("2006-01-02 15:04:05"), os.Getpid())
}

// Close closes the shared log file if it is open.
func Close() {
	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}

// Debugf appends a debug line to the shared asm log.
func Debugf(format string, args ...any) {
	write("DEBUG", fmt.Sprintf(format, args...))
}

// Errorf mirrors the message to stderr and appends it to the shared asm log.
func Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprint(os.Stderr, msg)
	write("ERROR", msg)
}

func write(level, msg string) {
	mu.Lock()
	defer mu.Unlock()

	if !ensureOpenLocked() {
		return
	}
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	fmt.Fprintf(logFile, "%s [%s] %s",
		time.Now().Format("2006-01-02 15:04:05"), level, msg)
}

func ensureOpenLocked() bool {
	if logFile != nil {
		return true
	}
	logDir := filepath.Dir(Path())
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return false
	}
	f, err := os.OpenFile(Path(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return false
	}
	logFile = f
	return true
}
