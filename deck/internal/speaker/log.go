package speaker

import (
	"fmt"
	"log"
	"strings"

	librespot "github.com/devgianlu/go-librespot"
)

// logger is the logger of go-librespot (librespot.Logger) on the log of the deck, as
// cmd/daemon/log.go of go-librespot does with logrus. It writes info and higher, and debug when
// it is on.
type logger struct {
	debug  bool
	fields string
}

func newLogger(debug bool) logger { return logger{debug: debug} }

func (l logger) out(level, msg string) {
	log.Printf("spotify %s: %s%s", level, msg, l.fields)
}

func (l logger) Tracef(format string, args ...interface{}) {}
func (l logger) Debugf(format string, args ...interface{}) {
	if l.debug {
		l.out("debug", fmt.Sprintf(format, args...))
	}
}
func (l logger) Infof(format string, args ...interface{}) {
	l.out("info", fmt.Sprintf(format, args...))
}
func (l logger) Warnf(format string, args ...interface{}) {
	l.out("warn", fmt.Sprintf(format, args...))
}
func (l logger) Errorf(format string, args ...interface{}) {
	l.out("error", fmt.Sprintf(format, args...))
}
func (l logger) Trace(args ...interface{}) {}
func (l logger) Debug(args ...interface{}) {
	if l.debug {
		l.out("debug", strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
	}
}
func (l logger) Info(args ...interface{}) {
	l.out("info", strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
}
func (l logger) Warn(args ...interface{}) {
	l.out("warn", strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
}
func (l logger) Error(args ...interface{}) {
	l.out("error", strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
}

func (l logger) WithField(key string, value interface{}) librespot.Logger {
	l.fields += fmt.Sprintf(" %s=%v", key, value)
	return l
}

func (l logger) WithError(err error) librespot.Logger {
	l.fields += fmt.Sprintf(" error=%v", err)
	return l
}
