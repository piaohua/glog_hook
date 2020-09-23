// Go support for leveled logs, analogous to https://code.google.com/p/google-glog/
//
// Copyright 2013 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hook

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
)

// flushSyncWriter is the interface satisfied by logging destinations.
type flushSyncWriter interface {
	Flush() error
	Sync() error
	io.Writer
}

// loggingT collects all the global state of the logging setup.
type loggingT struct {
	// mu protects the remaining elements of this structure and is
	// used to synchronize logging.
	mu sync.Mutex
	// file holds writer for each of the log types.
	file [7]flushSyncWriter
}

// output writes the data to the log files and releases the buffer.
func (l *loggingT) output(s logrus.Level, data []byte) {
	l.mu.Lock()
	if l.file[s] == nil {
		if err := l.createFiles(s); err != nil {
			os.Stderr.Write(data) // Make sure the message appears somewhere.
			l.exit(err)
		}
	}
	switch s {
	case logrus.PanicLevel:
		l.file[logrus.PanicLevel].Write(data)
		fallthrough
	case logrus.FatalLevel:
		l.file[logrus.FatalLevel].Write(data)
		fallthrough
	case logrus.ErrorLevel:
		l.file[logrus.ErrorLevel].Write(data)
		fallthrough
	case logrus.WarnLevel:
		l.file[logrus.WarnLevel].Write(data)
		fallthrough
	case logrus.InfoLevel:
		l.file[logrus.InfoLevel].Write(data)
		fallthrough
	case logrus.DebugLevel:
		l.file[logrus.DebugLevel].Write(data)
		fallthrough
	case logrus.TraceLevel:
		l.file[logrus.TraceLevel].Write(data)
	}
	if s <= logrus.FatalLevel {
		// Write the stack trace for all goroutines to the files.
		trace := stacks(true)
		for ss := s; ss <= logrus.TraceLevel; ss++ {
			if f := l.file[ss]; f != nil {
				f.Write(trace)
			}
		}
		l.mu.Unlock()
		l.timeoutFlush(10 * time.Second)
		return
	}
	l.mu.Unlock()
}

// timeoutFlush calls Flush and returns when it completes or after timeout
// elapses, whichever happens first.  This is needed because the hooks invoked
// by Flush may deadlock when glog.Fatal is called from a hook that holds
// a lock.
func (l *loggingT) timeoutFlush(timeout time.Duration) {
	done := make(chan bool, 1)
	go func() {
		l.lockAndFlushAll() // calls logging.lockAndFlushAll()
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		fmt.Fprintln(os.Stderr, "Flush took longer than", timeout)
	}
}

// stacks is a wrapper for runtime.Stack that attempts to recover the data for all goroutines.
func stacks(all bool) []byte {
	// We don't know how big the traces are, so grow a few times if they don't fit. Start large, though.
	n := 10000
	if all {
		n = 100000
	}
	var trace []byte
	for i := 0; i < 5; i++ {
		trace = make([]byte, n)
		nbytes := runtime.Stack(trace, all)
		if nbytes < len(trace) {
			return trace[:nbytes]
		}
		n *= 2
	}
	return trace
}

// syncBuffer joins a bufio.Writer to its underlying file, providing access to the
// file's Sync method and providing a wrapper for the Write method that provides log
// file rotation. There are conflicting methods, so the file cannot be embedded.
// l.mu is held for all its methods.
type syncBuffer struct {
	logger *loggingT
	*bufio.Writer
	file   *os.File
	sev    logrus.Level
	nbytes uint64 // The number of bytes written to this file
}

func (sb *syncBuffer) Sync() error {
	return sb.file.Sync()
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	if sb.nbytes+uint64(len(p)) >= MaxSize {
		if err := sb.rotateFile(time.Now()); err != nil {
			sb.logger.exit(err)
		}
	}
	n, err = sb.Writer.Write(p)
	sb.nbytes += uint64(n)
	if err != nil {
		sb.logger.exit(err)
	}
	return
}

// rotateFile closes the syncBuffer's file and starts a new one.
func (sb *syncBuffer) rotateFile(now time.Time) error {
	if sb.file != nil {
		sb.Flush()
		sb.file.Close()
	}
	var err error
	sb.file, _, err = create(sb.sev.String(), now)
	sb.nbytes = 0
	if err != nil {
		return err
	}

	sb.Writer = bufio.NewWriterSize(sb.file, bufferSize)
	return err
}

// bufferSize sizes the buffer associated with each log file. It's large
// so that log records can accumulate without the logging thread blocking
// on disk I/O. The flushDaemon will block instead.
const bufferSize = 256 * 1024

// createFiles creates all the log files for severity from sev down to infoLog.
// l.mu is held.
func (l *loggingT) createFiles(sev logrus.Level) error {
	now := time.Now()
	// Files are created in decreasing severity order, so as soon as we find one
	// has already been created, we can stop.
	for s := sev; s <= logrus.TraceLevel && l.file[s] == nil; s++ {
		sb := &syncBuffer{
			logger: l,
			sev:    s,
		}
		if err := sb.rotateFile(now); err != nil {
			return err
		}
		l.file[s] = sb
	}
	return nil
}

const flushInterval = 30 * time.Second

// flushDaemon periodically flushes the log file buffers.
func (l *loggingT) flushDaemon() {
	for _ = range time.NewTicker(flushInterval).C {
		l.lockAndFlushAll()
	}
}

// lockAndFlushAll is like flushAll but locks l.mu first.
func (l *loggingT) lockAndFlushAll() {
	l.mu.Lock()
	l.flushAll()
	l.mu.Unlock()
}

// flushAll flushes all the logs and attempts to "sync" their data to disk.
// l.mu is held.
func (l *loggingT) flushAll() {
	// Flush from fatal down, in case there's trouble flushing.
	for s := logrus.PanicLevel; s <= logrus.TraceLevel; s++ {
		file := l.file[s]
		if file != nil {
			file.Flush() // ignore error
			file.Sync()  // ignore error
		}
	}
}

// exit is called if there is trouble creating or writing log files.
// It flushes the logs and exits the program; there's no point in hanging around.
// l.mu is held.
func (l *loggingT) exit(err error) {
	fmt.Fprintf(os.Stderr, "log: exiting because of error: %s\n", err)
	l.flushAll()
	os.Exit(2)
}
