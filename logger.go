package main

import (
	"fmt"
	"io"
	"os"
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

// Color returns the ANSI color code for the log level
func (l LogLevel) Color() string {
	switch l {
	case DEBUG:
		return "\033[36m" // Cyan
	case INFO:
		return "\033[32m" // Green
	case WARN:
		return "\033[33m" // Yellow
	case ERROR:
		return "\033[31m" // Red
	default:
		return "\033[0m" // Reset
	}
}

// Logger handles structured logging
type Logger struct {
	mu       sync.Mutex
	level    LogLevel
	output   io.Writer
	quiet    bool
	stats    *ErrorStats
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

// NewLogger creates a new logger with the specified level
func NewLogger(level LogLevel) *Logger {
	return &Logger{
		level:  level,
		output: os.Stderr,
		stats:  &ErrorStats{ByType: make(map[string]int)},
	}
}

// SetOutput sets the output destination for log messages
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
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
func (l *Logger) log(level LogLevel, msg string, err error, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if we should log this level
	if level < l.level {
		return
	}

	// In quiet mode, only show errors
	if l.quiet && level < ERROR {
		return
	}

	// Update stats
	if level >= ERROR {
		l.stats.mu.Lock()
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
		l.stats.mu.Unlock()
	} else if level == WARN {
		l.stats.mu.Lock()
		l.stats.TotalWarnings++
		l.stats.mu.Unlock()
	}

	// Build log message
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	color := level.Color()
	reset := "\033[0m"

	message := fmt.Sprintf("%s [%s%s%s] %s", timestamp, color, level.String(), reset, msg)

	// Add error if present
	if err != nil {
		message += fmt.Sprintf(": %v", err)
	}

	// Add fields if present
	if len(fields) > 0 {
		message += " {"
		first := true
		for k, v := range fields {
			if !first {
				message += ", "
			}
			message += fmt.Sprintf("%s: %v", k, v)
			first = false
		}
		message += "}"
	}

	// Write output
	fmt.Fprintln(l.output, message)
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	l.log(DEBUG, msg, nil, fields)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	l.log(INFO, msg, nil, fields)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	l.log(WARN, msg, nil, fields)
}

// Error logs an error message
func (l *Logger) Error(msg string, err error, fields map[string]interface{}) {
	l.log(ERROR, msg, err, fields)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.log(DEBUG, fmt.Sprintf(format, args...), nil, nil)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.log(INFO, fmt.Sprintf(format, args...), nil, nil)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.log(WARN, fmt.Sprintf(format, args...), nil, nil)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.log(ERROR, fmt.Sprintf(format, args...), nil, nil)
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
func copyMap(m map[string]int) map[string]int {
	result := make(map[string]int, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
