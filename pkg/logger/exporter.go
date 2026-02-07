package logger

import (
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// MultiWriter is a writer that writes to multiple writers.
type MultiWriter struct {
	writers []io.Writer
	mu      sync.Mutex
}

// NewMultiWriter creates a new MultiWriter.
func NewMultiWriter(writers ...io.Writer) *MultiWriter {
	return &MultiWriter{writers: writers}
}

// Write writes the same data to all writers.
func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	for _, w := range mw.writers {
		n, err = w.Write(p)
		if err != nil {
			return n, err
		}
		if n != len(p) {
			return n, io.ErrShortWrite
		}
	}
	return len(p), nil
}

// AddWriter adds a writer to the MultiWriter.
func (mw *MultiWriter) AddWriter(w io.Writer) {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	mw.writers = append(mw.writers, w)
}

// RemoveWriter removes a writer from the MultiWriter.
func (mw *MultiWriter) RemoveWriter(w io.Writer) {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	for i, writer := range mw.writers {
		if writer == w {
			mw.writers = append(mw.writers[:i], mw.writers[i+1:]...)
			return
		}
	}
}

// FileRotationHook is an io.Writer that rotates log files.
type FileRotationHook struct {
	file         *os.File
	filename     string
	maxSize      int64
	rotateTime   time.Duration
	lastRotation time.Time
	currentSize  int64
	mu           sync.Mutex
}

// NewFileRotationHook creates a new file rotation writer.
func NewFileRotationHook(filename string, maxSizeMB int, rotateHours int) (*FileRotationHook, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return nil, err
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return &FileRotationHook{
		file:         file,
		filename:     filename,
		maxSize:      int64(maxSizeMB) * 1024 * 1024,
		rotateTime:   time.Duration(rotateHours) * time.Hour,
		lastRotation: time.Now(),
		currentSize:  stat.Size(),
	}, nil
}

// Write writes logs to file and rotates when threshold reached.
func (hook *FileRotationHook) Write(p []byte) (n int, err error) {
	hook.mu.Lock()
	defer hook.mu.Unlock()

	if hook.shouldRotate() {
		if err := hook.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = hook.file.Write(p)
	if err != nil {
		return n, err
	}
	hook.currentSize += int64(n)
	return n, nil
}

func (hook *FileRotationHook) shouldRotate() bool {
	return hook.currentSize >= hook.maxSize || time.Since(hook.lastRotation) >= hook.rotateTime
}

func (hook *FileRotationHook) rotate() error {
	if err := hook.file.Close(); err != nil {
		return err
	}

	timestamp := time.Now().Format("20060102-150405")
	if err := os.Rename(hook.filename, hook.filename+"."+timestamp); err != nil {
		return err
	}

	file, err := os.OpenFile(hook.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return err
	}

	hook.file = file
	hook.currentSize = 0
	hook.lastRotation = time.Now()
	return nil
}

// Close closes the file.
func (hook *FileRotationHook) Close() error {
	hook.mu.Lock()
	defer hook.mu.Unlock()
	if hook.file != nil {
		return hook.file.Close()
	}
	return nil
}

// AddFileLogger adds a rotating file output while preserving current output.
func AddFileLogger(filename string, maxSizeMB int, rotateHours int) error {
	hook, err := NewFileRotationHook(filename, maxSizeMB, rotateHours)
	if err != nil {
		return err
	}

	logMu.Lock()
	defer logMu.Unlock()

	mw := NewMultiWriter(currentOut, hook)
	currentOut = mw
	baseLogger = slog.New(newHandler(currentOut, currentConf))
	return nil
}
