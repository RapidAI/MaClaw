package tinytex

import "errors"

var (
	errNoEngine       = errors.New("xelatex not found")
	errCompileFailed  = errors.New("latex compile failed")
	errSourceTooLarge = errors.New("latex source too large to repair")
)
