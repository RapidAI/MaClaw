package structureddata

import "errors"

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
