package hook

import (
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
)

// GlogHook to send logs via logger.
type GlogHook struct {
	formatter logrus.Formatter
	logger    *loggingT
}

// NewGlogHook Creates a hook to be added to an instance of logger.
// This is called with
// `hook := NewGlogHook(logrus.JSONFormatter{})`
// `if err == nil { log.Hooks.Add(hook) }`
func NewGlogHook(f logrus.Formatter) logrus.Hook {
	l := &loggingT{}
	go l.flushDaemon()
	return &GlogHook{
		formatter: f,
		logger:    l,
	}
}

// Fire takes, formats and sends the entry to logger.
func (hook *GlogHook) Fire(entry *logrus.Entry) error {
	body, err := hook.formatter.Format(entry)
	if err != nil {
		return err
	}

	hook.logger.output(entry.Level, body)
	return err
}

// Levels returns all logrus levels.
func (hook *GlogHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Using a pool to re-use of old entries when formatting Glog messages.
// It is used in the Fire function.
var entryPool = sync.Pool{
	New: func() interface{} {
		l := logrus.StandardLogger()
		return logrus.NewEntry(l)
	},
}

// copyEntry copies the entry `e` to a new entry and then adds all the fields in `fields` that are missing in the new entry data.
// It uses `entryPool` to re-use allocated entries.
func copyEntry(e *logrus.Entry, fields logrus.Fields) *logrus.Entry {
	ne := entryPool.Get().(*logrus.Entry)
	ne.Message = e.Message
	ne.Level = e.Level
	ne.Time = e.Time
	ne.Data = e.Data
	ne.Caller = e.Caller
	for k, v := range fields {
		ne.Data[k] = v
	}
	for k, v := range e.Data {
		ne.Data[k] = v
	}
	ne.Data["localtime"] = time.Now().Local().Format(time.RFC3339)
	return ne
}

// releaseEntry puts the given entry back to `entryPool`. It must be called if copyEntry is called.
func releaseEntry(e *logrus.Entry) {
	entryPool.Put(e)
}

// GlogFormatter represents a Publish format.
// It has logrus.Formatter which formats the entry and logrus.Fields which
// are added to the JSON message if not given in the entry data.
//
// Note: use the `DefaultFormatter` function to set a default Publish formatter.
type GlogFormatter struct {
	logrus.Formatter
	logrus.Fields
}

var (
	publishFields   = logrus.Fields{"@version": "1", "type": "log"}
	publishFieldMap = logrus.FieldMap{
		logrus.FieldKeyTime:  "@timestamp",
		logrus.FieldKeyLevel: "@level",
		logrus.FieldKeyMsg:   "@message",
		logrus.FieldKeyFunc:  "@caller",
		logrus.FieldKeyFile:  "@file",
	}
)

// DefaultFormatter returns a default Publish formatter:
// A JSON format with "@version" set to "1" (unless set differently in `fields`,
// "type" to "log" (unless set differently in `fields`),
// "@timestamp" to the log time and "message" to the log message.
//
// Note: to set a different configuration use the `GlogFormatter` structure.
func DefaultFormatter(fields logrus.Fields) logrus.Formatter {
	for k, v := range publishFields {
		if _, ok := fields[k]; !ok {
			fields[k] = v
		}
	}

	return GlogFormatter{
		Formatter: &logrus.JSONFormatter{FieldMap: publishFieldMap},
		Fields:    fields,
	}
}

// Format formats an entry to a Publish format according to the given Formatter and Fields.
//
// Note: the given entry is copied and not changed during the formatting process.
func (f GlogFormatter) Format(e *logrus.Entry) ([]byte, error) {
	ne := copyEntry(e, f.Fields)
	dataBytes, err := f.Formatter.Format(ne)
	releaseEntry(ne)
	return dataBytes, err
}
