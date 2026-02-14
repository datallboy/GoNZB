package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

type Logger struct {
	fileLogger    *log.Logger
	level         Level
	includeStdout bool
}

func New(filePath string, level Level, includeStdout bool) (*Logger, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &Logger{
		fileLogger:    log.New(f, "", 0),
		level:         level,
		includeStdout: includeStdout,
	}, nil
}

func (l *Logger) log(lvl Level, prefix string, format string, v ...interface{}) {
	if lvl < l.level {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, v...)
	fullMsg := fmt.Sprintf("%s [%s] %s", timestamp, prefix, msg)

	l.fileLogger.Println(fullMsg)

	// Write to Stdout for Docker/CLI if enabled AND level is Info or higher
	// This prevents Debug spam from breaking progress bar and other CLI UI elements
	if l.includeStdout && lvl >= LevelInfo {
		fmt.Printf("\n%s", fullMsg)
	}
}

func ParseLevel(lvl string) Level {
	switch strings.ToLower(lvl) {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l *Logger) Debug(f string, v ...any) { l.log(LevelDebug, "DEBUG", f, v...) }
func (l *Logger) Info(f string, v ...any)  { l.log(LevelInfo, "INFO", f, v...) }
func (l *Logger) Warn(f string, v ...any)  { l.log(LevelWarn, "WARN", f, v...) }
func (l *Logger) Error(f string, v ...any) { l.log(LevelError, "ERROR", f, v...) }
func (l *Logger) Fatal(f string, v ...any) { l.log(LevelFatal, "FATAL", f, v...); os.Exit(1) }

func (l *Logger) Write(p []byte) (n int, err error) {
	// Echo and other libraries often include a newline at the end
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		l.Info("%s", msg)
	}
	return len(p), nil
}
