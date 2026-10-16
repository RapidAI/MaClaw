package agentruntime

import (
	"context"
	"errors"
	"reflect"
)

// EventSinkFunc adapts a function to the shared EventSink contract.
type EventSinkFunc func(context.Context, Event) error

func (f EventSinkFunc) Emit(ctx context.Context, event Event) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

// FanoutEventSink delivers each event to every configured sink. It is useful
// when a host needs both durable outbox persistence and an immediate UI/SSE
// projection. All sinks are attempted even when one fails; callers receive an
// errors.Join result so a failed durable sink cannot be hidden by a healthy
// presentation sink.
type FanoutEventSink struct {
	Sinks []EventSink
}

func (f FanoutEventSink) Emit(ctx context.Context, event Event) error {
	var errs []error
	for _, sink := range f.Sinks {
		if isNilEventSink(sink) {
			continue
		}
		if err := sink.Emit(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func isNilEventSink(sink EventSink) bool {
	if sink == nil {
		return true
	}
	value := reflect.ValueOf(sink)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
