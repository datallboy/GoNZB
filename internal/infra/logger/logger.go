package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	fileWriter    io.Closer
	level         Level
	includeStdout bool
}

type Options struct {
	MaxSizeMB  int
	MaxBackups int
}

const (
	defaultMaxSizeMB  = 32
	defaultMaxBackups = 5
)

func New(filePath string, level Level, includeStdout bool) (*Logger, error) {
	return NewWithOptions(filePath, level, includeStdout, Options{})
}

func NewWithOptions(filePath string, level Level, includeStdout bool, opts Options) (*Logger, error) {
	var fileLogger *log.Logger
	var fileWriter io.Closer

	if filePath != "" && filePath != "/dev/null" && !strings.EqualFold(filePath, "none") {
		w, err := newRotatingFileWriter(filePath, opts)
		if err != nil {
			return nil, err
		}
		fileWriter = w
		fileLogger = log.New(w, "", 0)
	}

	return &Logger{
		fileLogger:    fileLogger,
		fileWriter:    fileWriter,
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

	if l.fileLogger != nil {
		l.fileLogger.Println(fullMsg)
	}

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

func (l *Logger) Close() error {
	if l == nil || l.fileWriter == nil {
		return nil
	}
	return l.fileWriter.Close()
}

func (l *Logger) Write(p []byte) (n int, err error) {
	// Echo and other libraries often include a newline at the end
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		l.Info("%s", msg)
	}
	return len(p), nil
}

type rotatingFileWriter struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
	file       *os.File
	size       int64
}

func newRotatingFileWriter(path string, opts Options) (*rotatingFileWriter, error) {
	maxSizeMB := opts.MaxSizeMB
	if maxSizeMB <= 0 {
		maxSizeMB = defaultMaxSizeMB
	}
	maxBackups := opts.MaxBackups
	if maxBackups <= 0 {
		maxBackups = defaultMaxBackups
	}

	w := &rotatingFileWriter{
		path:       path,
		maxBytes:   int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.maxBytes > 0 && w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingFileWriter) open() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = file
	if st, err := file.Stat(); err == nil {
		w.size = st.Size()
	} else {
		w.size = 0
	}
	return nil
}

func (w *rotatingFileWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}

	if w.maxBackups > 0 {
		for i := w.maxBackups - 1; i >= 1; i-- {
			oldPath := fmt.Sprintf("%s.%d", w.path, i)
			newPath := fmt.Sprintf("%s.%d", w.path, i+1)
			_ = os.Rename(oldPath, newPath)
		}
		_ = os.Rename(w.path, fmt.Sprintf("%s.1", w.path))
	} else {
		_ = os.Remove(w.path)
	}

	return w.open()
}
