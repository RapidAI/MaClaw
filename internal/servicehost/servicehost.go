//go:build !windows

package servicehost

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// Run starts fn as a normal command-line process and cancels its context when
// the process receives an interrupt or termination signal.
func Run(_ string, fn func(context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return fn(ctx)
}
