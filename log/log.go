//go:build !ReleaseFast

package log

import (
	"fmt"
	"log"
)

func Trace(format string, v ...any) {
	log.Output(2, fmt.Sprintf("trace "+format, v...))
}

func Debug(format string, v ...any) {
	log.Output(2, fmt.Sprintf("debug "+format, v...))
}

func Info(format string, v ...any) {
	log.Output(2, fmt.Sprintf("info "+format, v...))
}

func Warn(format string, v ...any) {
	log.Output(2, fmt.Sprintf("warn "+format, v...))
}

func Error(format string, v ...any) {
	log.Output(2, fmt.Sprintf("error "+format, v...))
}
