package main

import "github.com/nhn/asm/asmlog"

func initLog() {
	asmlog.Init()
}

func closeLog() {
	asmlog.Close()
}

func logErr(format string, args ...any) {
	asmlog.Errorf(format, args...)
}

func logDebug(format string, args ...any) {
	asmlog.Debugf(format, args...)
}
