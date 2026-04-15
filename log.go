package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nhn/asm/config"
)

var logFile *os.File

func initLog() {
	logDir := filepath.Join(config.UserConfigDir(), "logs")
	os.MkdirAll(logDir, 0755)
	f, err := os.OpenFile(filepath.Join(logDir, "asm.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	logFile = f
	fmt.Fprintf(logFile, "\n=== asm started at %s ===\n", time.Now().Format("2006-01-02 15:04:05"))
}

func closeLog() {
	if logFile != nil {
		logFile.Close()
	}
}

func logErr(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprint(os.Stderr, msg)
	if logFile != nil {
		fmt.Fprintf(logFile, "%s %s", time.Now().Format("2006-01-02 15:04:05"), msg)
	}
}
