package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger handles structured logging
type Logger struct {
	mu     sync.Mutex
	level  LogLevel
	output io.Writer
	quiet  bool
	stats  *ErrorStats
	file   *os.File
	logger *slog.Logger
}

// ErrorStats tracks error statistics
type ErrorStats struct {
	mu            sync.Mutex
	TotalErrors   int
	TotalWarnings int
	ByType        map[string]int
	LastError     string
	LastErrorTime time.Time
}

func newDefaultLogger(level LogLevel) *Logger {
	logger := &Logger{
		level:  level,
		output: os.Stderr,
		stats:  &ErrorStats{ByType: make(map[string]int)},
	}
	logger.rebuildSlogLoggerLocked()
	return logger
}

// NewLogger creates a new logger with the specified level
// Tries to write to /tmp/better_diff.log first, then falls back to repo root.
// If both fail, uses stderr and returns an error
func NewLogger(level LogLevel, gitRootPath string) (*Logger, error) {
	logger := newDefaultLogger(level)

	// Try /tmp first
	logFilePath := "/tmp/better_diff.log"
	pathsTried := []string{logFilePath}
	file, err := openLogFile(logFilePath)
	if err != nil {
		// Fall back to git repository root.
		if gitRootPath != "" {
			logFilePath = filepath.Join(gitRootPath, "better_diff.log")
			pathsTried = append(pathsTried, logFilePath)
			file, err = openLogFile(logFilePath)
			if err == nil {
				logger.output = file
				logger.file = file
				logger.rebuildSlogLoggerLocked()
				return logger, nil
			}
		}
		// If both fail, return logger with stderr but also return error
		return logger, fmt.Errorf("failed to open log file (tried %s): %w", strings.Join(pathsTried, ", "), err)
	}

	logger.output = file
	logger.file = file
	logger.rebuildSlogLoggerLocked()
	return logger, nil
}

func openLogFile(path string) (*os.File, error) {
	const logFilePermission = 0o644
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePermission)
}

func (l *Logger) rebuildSlogLoggerLocked() {
	l.logger = slog.New(slog.NewTextHandler(l.output, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// SetOutput sets the output destination for log messages
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
	l.rebuildSlogLoggerLocked()
}

// SetLevel sets the minimum log level
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetQuiet enables or disables quiet mode (only errors)
func (l *Logger) SetQuiet(quiet bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.quiet = quiet
}

// GetStats returns a copy of the error statistics
func (l *Logger) GetStats() ErrorStats {
	l.stats.mu.Lock()
	defer l.stats.mu.Unlock()

	return ErrorStats{
		TotalErrors:   l.stats.TotalErrors,
		TotalWarnings: l.stats.TotalWarnings,
		ByType:        copyMap(l.stats.ByType),
		LastError:     l.stats.LastError,
		LastErrorTime: l.stats.LastErrorTime,
	}
}

// log is the internal logging method
func (l *Logger) log(level LogLevel, msg string, err error, fields map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.shouldLog(level) {
		return
	}

	l.updateStats(level, msg, err)

	args := make([]any, 0, len(fields)+2)
	if err != nil {
		args = append(args, "error", err)
	}
	for _, key := range sortedFieldKeys(fields) {
		args = append(args, key, fields[key])
	}

	l.logger.Log(context.Background(), toSlogLevel(level), msg, args...)
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]any) {
	l.log(DEBUG, msg, nil, fields)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]any) {
	l.log(INFO, msg, nil, fields)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]any) {
	l.log(WARN, msg, nil, fields)
}

// Error logs an error message
func (l *Logger) Error(msg string, err error, fields map[string]any) {
	l.log(ERROR, msg, err, fields)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...any) {
	l.logf(DEBUG, format, args...)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...any) {
	l.logf(INFO, format, args...)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...any) {
	l.logf(WARN, format, args...)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...any) {
	l.logf(ERROR, format, args...)
}

func (l *Logger) logf(level LogLevel, format string, args ...any) {
	l.log(level, fmt.Sprintf(format, args...), nil, nil)
}

func (l *Logger) shouldLog(level LogLevel) bool {
	if level < l.level {
		return false
	}
	if l.quiet && level < ERROR {
		return false
	}
	return true
}

func (l *Logger) updateStats(level LogLevel, msg string, err error) {
	l.stats.mu.Lock()
	defer l.stats.mu.Unlock()

	if level >= ERROR {
		l.stats.TotalErrors++
		if err != nil {
			errType := fmt.Sprintf("%T", err)
			l.stats.ByType[errType]++
		}
		l.stats.LastError = msg
		if err != nil {
			l.stats.LastError += ": " + err.Error()
		}
		l.stats.LastErrorTime = time.Now()
		return
	}

	if level == WARN {
		l.stats.TotalWarnings++
	}
}

func toSlogLevel(level LogLevel) slog.Level {
	switch level {
	case DEBUG:
		return slog.LevelDebug
	case INFO:
		return slog.LevelInfo
	case WARN:
		return slog.LevelWarn
	case ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// HasErrors returns true if any errors have been logged
func (l *Logger) HasErrors() bool {
	l.stats.mu.Lock()
	defer l.stats.mu.Unlock()
	return l.stats.TotalErrors > 0
}

// GetErrorCount returns the total number of errors logged
func (l *Logger) GetErrorCount() int {
	l.stats.mu.Lock()
	defer l.stats.mu.Unlock()
	return l.stats.TotalErrors
}

// GetWarningCount returns the total number of warnings logged
func (l *Logger) GetWarningCount() int {
	l.stats.mu.Lock()
	defer l.stats.mu.Unlock()
	return l.stats.TotalWarnings
}

// Reset clears all statistics
func (l *Logger) Reset() {
	l.stats.mu.Lock()
	defer l.stats.mu.Unlock()
	l.stats.TotalErrors = 0
	l.stats.TotalWarnings = 0
	l.stats.ByType = make(map[string]int)
	l.stats.LastError = ""
	l.stats.LastErrorTime = time.Time{}
}

// helper function to copy a map
// Close closes the log file if one is open
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}
