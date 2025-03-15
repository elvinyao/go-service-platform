package logger

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LogHook is an interface for log hooks that can be added to the logger
type LogHook interface {
	Levels() []logrus.Level
	Fire(*logrus.Entry) error
}

// MultiWriter is a writer that writes to multiple writers
type MultiWriter struct {
	writers []io.Writer
	mu      sync.Mutex
}

// NewMultiWriter creates a new MultiWriter
func NewMultiWriter(writers ...io.Writer) *MultiWriter {
	return &MultiWriter{
		writers: writers,
	}
}

// Write writes the same data to all writers
func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	for _, w := range mw.writers {
		n, err = w.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

// AddWriter adds a writer to the MultiWriter
func (mw *MultiWriter) AddWriter(w io.Writer) {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	mw.writers = append(mw.writers, w)
}

// RemoveWriter removes a writer from the MultiWriter
func (mw *MultiWriter) RemoveWriter(w io.Writer) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	for i, writer := range mw.writers {
		if writer == w {
			mw.writers = append(mw.writers[:i], mw.writers[i+1:]...)
			break
		}
	}
}

// FileRotationHook is a hook that rotates log files
type FileRotationHook struct {
	file         *os.File
	filename     string
	maxSize      int64
	rotateTime   time.Duration
	lastRotation time.Time
	currentSize  int64
	mu           sync.Mutex
}

// NewFileRotationHook creates a new file rotation hook
func NewFileRotationHook(filename string, maxSizeMB int, rotateHours int) (*FileRotationHook, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	hook := &FileRotationHook{
		file:         file,
		filename:     filename,
		maxSize:      int64(maxSizeMB) * 1024 * 1024, // Convert MB to bytes
		rotateTime:   time.Duration(rotateHours) * time.Hour,
		lastRotation: time.Now(),
		currentSize:  stat.Size(),
	}

	return hook, nil
}

// Fire is called when a log event occurs
func (hook *FileRotationHook) Fire(entry *logrus.Entry) error {
	hook.mu.Lock()
	defer hook.mu.Unlock()

	// Check if the file needs to be rotated
	if hook.shouldRotate() {
		if err := hook.rotate(); err != nil {
			return err
		}
	}

	// Format the log entry
	data, err := entry.Logger.Formatter.Format(entry)
	if err != nil {
		return err
	}

	// Write to the file
	n, err := hook.file.Write(data)
	if err != nil {
		return err
	}

	// Update current size
	hook.currentSize += int64(n)

	return nil
}

// shouldRotate checks if the file should be rotated
func (hook *FileRotationHook) shouldRotate() bool {
	return hook.currentSize >= hook.maxSize ||
		time.Since(hook.lastRotation) >= hook.rotateTime
}

// rotate rotates the log file
func (hook *FileRotationHook) rotate() error {
	// Close the current file
	if err := hook.file.Close(); err != nil {
		return err
	}

	// Rename the current file
	timestamp := time.Now().Format("20060102-150405")
	if err := os.Rename(hook.filename, hook.filename+"."+timestamp); err != nil {
		return err
	}

	// Open a new file
	file, err := os.OpenFile(hook.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	// Update the hook's file and stats
	hook.file = file
	hook.currentSize = 0
	hook.lastRotation = time.Now()

	return nil
}

// Levels returns the logrus levels that this hook should be called for
func (hook *FileRotationHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Close closes the file hook
func (hook *FileRotationHook) Close() error {
	hook.mu.Lock()
	defer hook.mu.Unlock()

	if hook.file != nil {
		return hook.file.Close()
	}
	return nil
}

// AddFileLogger adds a rotating file logger to the logger
func AddFileLogger(filename string, maxSizeMB int, rotateHours int) error {
	hook, err := NewFileRotationHook(filename, maxSizeMB, rotateHours)
	if err != nil {
		return err
	}

	log.AddHook(hook)
	return nil
}
