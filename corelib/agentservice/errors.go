package agentservice

import "errors"

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrTenantNotFound     = errors.New("tenant not found")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserConfigNotFound = errors.New("user config not found")
	ErrInstanceNotFound   = errors.New("instance not found")
	ErrSessionNotFound    = errors.New("session not found")
	ErrRunNotFound        = errors.New("run not found")
	ErrRunNotRunning      = errors.New("run is not running")
	ErrInstanceBusy       = errors.New("instance has running runs")
	ErrUserBusy           = errors.New("user has running runs")
	ErrTenantBusy         = errors.New("tenant has running runs")
	ErrDeleteProtected    = errors.New("resource is delete-protected")
	ErrSessionBusy        = errors.New("session has running runs")
	ErrSessionArchived    = errors.New("session is archived")
	ErrCredentialNotFound = errors.New("credential not found")
	ErrQuotaExceeded      = errors.New("quota exceeded")
	ErrInvalidConfig      = errors.New("invalid config")
	ErrAlreadyExists      = errors.New("resource already exists")
)
