//go:build windows

package servicehost

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows/svc"
)

// Run starts fn either as a normal command-line process or, when invoked by the
// Windows Service Control Manager, as an NT service.
func Run(serviceName string, fn func(context.Context) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if isService {
		return svc.Run(serviceName, serviceHandler{run: fn})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return fn(ctx)
}

type serviceHandler struct {
	run func(context.Context) error
}

func (h serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	changes <- svc.Status{State: svc.StartPending}
	go func() {
		errCh <- h.run(ctx)
	}()
	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case err := <-errCh:
			changes <- svc.Status{State: svc.StopPending}
			changes <- svc.Status{State: svc.Stopped}
			if err != nil {
				return true, 1
			}
			return false, 0
		case request, ok := <-requests:
			if !ok {
				cancel()
				if err := <-errCh; err != nil {
					changes <- svc.Status{State: svc.Stopped}
					return true, 1
				}
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-errCh; err != nil {
					changes <- svc.Status{State: svc.Stopped}
					return true, 1
				}
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
			}
		}
	}
}
