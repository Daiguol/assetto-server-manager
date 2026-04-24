// Package logrus is a thin shim that exposes the slice of sirupsen/logrus's
// API the codebase actually uses, but backed by stdlib log/slog underneath.
//
// Why a shim with the same name instead of a clean migration to slog:
// logrus is used in 450+ call-sites with the WithError(...).Errorf(...)
// chaining pattern that slog deliberately doesn't support natively. A
// straight rewrite would touch every one of them. Keeping the package name
// and the function surface means the migration is a single import-path
// rewrite; call-sites are untouched. Once the dep is gone we can migrate
// files to slog-native gradually, or not at all if this works well enough.
//
// Output format is close to logrus's default TextFormatter (lowercase
// level, RFC3339 time without sub-second precision) so `docker logs acsm`
// still looks familiar.
package logrus

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Level mirrors logrus.Level for the handful of constants the codebase
// actually references.
type Level uint32

const (
	PanicLevel Level = iota
	FatalLevel
	ErrorLevel
	WarnLevel
	InfoLevel
	DebugLevel
	TraceLevel
)

func (l Level) slogLevel() slog.Level {
	switch l {
	case DebugLevel, TraceLevel:
		return slog.LevelDebug
	case WarnLevel:
		return slog.LevelWarn
	case ErrorLevel, FatalLevel, PanicLevel:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

var (
	mu       sync.Mutex
	levelVar           = new(slog.LevelVar)
	output   io.Writer = os.Stdout
	current  *slog.Logger
	errorKey = "error"
)

func init() {
	levelVar.Set(slog.LevelInfo)
	current = slog.New(newHandler(output))
}

func newHandler(w io.Writer) slog.Handler {
	return slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: levelVar,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) != 0 {
				return a
			}
			switch a.Key {
			case slog.LevelKey:
				if lvl, ok := a.Value.Any().(slog.Level); ok {
					return slog.String(slog.LevelKey, strings.ToLower(lvl.String()))
				}
			case slog.TimeKey:
				return slog.String(slog.TimeKey, a.Value.Time().Format(time.RFC3339))
			}
			return a
		},
	})
}

// SetLevel mirrors logrus.SetLevel.
func SetLevel(l Level) {
	levelVar.Set(l.slogLevel())
}

// SetOutput mirrors logrus.SetOutput. Rebuilds the underlying logger so
// subsequent writes go to the new destination.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	output = w
	current = slog.New(newHandler(w))
}

func logger() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	return current
}

func logAt(lvl slog.Level, msg string, attrs ...any) {
	l := logger()
	if !l.Enabled(context.Background(), lvl) {
		return
	}
	l.Log(context.Background(), lvl, msg, attrs...)
}

// --- package-level logging API (no attrs attached) ---

func Info(args ...any)                  { logAt(slog.LevelInfo, fmt.Sprint(args...)) }
func Infof(format string, args ...any)  { logAt(slog.LevelInfo, fmt.Sprintf(format, args...)) }
func Debug(args ...any)                 { logAt(slog.LevelDebug, fmt.Sprint(args...)) }
func Debugf(format string, args ...any) { logAt(slog.LevelDebug, fmt.Sprintf(format, args...)) }
func Warn(args ...any)                  { logAt(slog.LevelWarn, fmt.Sprint(args...)) }
func Warnf(format string, args ...any)  { logAt(slog.LevelWarn, fmt.Sprintf(format, args...)) }
func Error(args ...any)                 { logAt(slog.LevelError, fmt.Sprint(args...)) }
func Errorf(format string, args ...any) { logAt(slog.LevelError, fmt.Sprintf(format, args...)) }

// Fatal logs at error level then exits the process. Matches logrus.Fatal.
func Fatal(args ...any) {
	logAt(slog.LevelError, fmt.Sprint(args...))
	os.Exit(1)
}

// Fatalf logs at error level then exits the process. Matches logrus.Fatalf.
func Fatalf(format string, args ...any) {
	logAt(slog.LevelError, fmt.Sprintf(format, args...))
	os.Exit(1)
}

// --- structured context: Entry ---

// Entry accumulates key/value pairs and emits them alongside the log
// message. Mirrors the subset of logrus.Entry that the codebase uses:
// WithError + WithField with a terminal Info/Infof/Debug/.../Errorf call.
type Entry struct {
	attrs []any
}

// WithError returns a new Entry that will emit the error under the "error"
// key when its terminal method is called.
func WithError(err error) *Entry {
	return &Entry{attrs: []any{errorKey, err}}
}

// WithField returns a new Entry with the given key/value attached.
func WithField(key string, value any) *Entry {
	return &Entry{attrs: []any{key, value}}
}

// WithError chains another "error" attribute onto the entry. Later
// WithError calls replace earlier ones to match logrus semantics.
func (e *Entry) WithError(err error) *Entry {
	return &Entry{attrs: append(append([]any{}, e.attrs...), errorKey, err)}
}

// WithField chains another key/value onto the entry.
func (e *Entry) WithField(key string, value any) *Entry {
	return &Entry{attrs: append(append([]any{}, e.attrs...), key, value)}
}

func (e *Entry) log(lvl slog.Level, msg string) {
	logAt(lvl, msg, e.attrs...)
}

func (e *Entry) Info(args ...any) { e.log(slog.LevelInfo, fmt.Sprint(args...)) }
func (e *Entry) Infof(format string, args ...any) {
	e.log(slog.LevelInfo, fmt.Sprintf(format, args...))
}
func (e *Entry) Debug(args ...any) { e.log(slog.LevelDebug, fmt.Sprint(args...)) }
func (e *Entry) Debugf(format string, args ...any) {
	e.log(slog.LevelDebug, fmt.Sprintf(format, args...))
}
func (e *Entry) Warn(args ...any) { e.log(slog.LevelWarn, fmt.Sprint(args...)) }
func (e *Entry) Warnf(format string, args ...any) {
	e.log(slog.LevelWarn, fmt.Sprintf(format, args...))
}
func (e *Entry) Error(args ...any) { e.log(slog.LevelError, fmt.Sprint(args...)) }
func (e *Entry) Errorf(format string, args ...any) {
	e.log(slog.LevelError, fmt.Sprintf(format, args...))
}

// Fatal logs then exits.
func (e *Entry) Fatal(args ...any) {
	e.log(slog.LevelError, fmt.Sprint(args...))
	os.Exit(1)
}

// Fatalf logs then exits.
func (e *Entry) Fatalf(format string, args ...any) {
	e.log(slog.LevelError, fmt.Sprintf(format, args...))
	os.Exit(1)
}
