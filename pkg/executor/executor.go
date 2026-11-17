package executor

import (
	"context"
	"errors"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type Executor interface {
	Type() string
	Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error
}

type permanentError struct {
	err error
}

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks an error as not worth retrying, such as invalid action
// parameters or a rejected request. The pipeline stops retrying an action as
// soon as its executor returns a permanent error.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err, or any error it wraps, was marked Permanent.
func IsPermanent(err error) bool {
	var target *permanentError
	return errors.As(err, &target)
}
