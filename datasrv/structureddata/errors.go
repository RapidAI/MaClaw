package structureddata

import (
	"errors"
	"strings"
)

var (
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrAdminNotFound   = errors.New("administrator not found")
	ErrSessionNotFound = errors.New("administrator session not found")
	ErrDatasetNotFound = errors.New("dataset not found")
	ErrRecordNotFound  = errors.New("record not found")
	ErrBackupNotFound  = errors.New("backup not found")
	ErrAlreadyExists   = errors.New("resource already exists")
	ErrInvalidInput    = errors.New("invalid input")
)

type businessError struct {
	BusinessError
	cause error
}

func (e *businessError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if e.cause == nil {
		return message
	}
	if message == "" {
		return e.cause.Error()
	}
	return e.cause.Error() + ": " + message
}

func (e *businessError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newBusinessError(cause error, code, message string) *businessError {
	return &businessError{
		BusinessError: BusinessError{Code: strings.TrimSpace(code), Message: strings.TrimSpace(message)},
		cause:         cause,
	}
}
