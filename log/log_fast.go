//go:build ReleaseFast

package log

import (
	"fmt"
	"log"
	"os"
)

var err = log.New(os.Stderr, "error", log.LstdFlags)

func Trace(format string, v ...any) {}

func Debug(format string, v ...any) {}

func Info(format string, v ...any) {}

func Warn(format string, v ...any) {}

func Error(format string, v ...any) {
	err.Output(2, fmt.Sprintf(format, v...))
}
