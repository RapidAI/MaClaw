package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var errWriteBatcherClosed = errors.New("sqlite write batcher is closed")

type writeBatchJob struct {
	ctx    context.Context
	query  string
	args   []any
	result chan error
}

type writeBatcher struct {
	db            *sql.DB
	flushInterval time.Duration
	maxBatchSize  int
	jobs          chan writeBatchJob
	stop          chan struct{}
	done          chan struct{}
}

func newWriteBatcher(db *sql.DB, cfg Config) *writeBatcher {
	flushInterval := time.Duration(cfg.BatchFlushMS) * time.Millisecond
	if flushInterval <= 0 {
		flushInterval = 250 * time.Millisecond
	}

	maxBatchSize := cfg.BatchMaxSize
	if maxBatchSize <= 0 {
		maxBatchSize = 64
	}

	queueSize := cfg.BatchQueueSize
	if queueSize <= 0 {
		queueSize = 1024
	}

	b := &writeBatcher{
		db:            db,
		flushInterval: flushInterval,
		maxBatchSize:  maxBatchSize,
		jobs:          make(chan writeBatchJob, queueSize),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go b.run()
	return b
}

func (b *writeBatcher) ExecContext(ctx context.Context, query string, args ...any) error {
	if b == nil {
		return errWriteBatcherClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}

	job := writeBatchJob{
		ctx:    ctx,
		query:  query,
		args:   append([]any(nil), args...),
		result: make(chan error, 1),
	}

	select {
	case <-b.stop:
		return errWriteBatcherClosed
	case <-ctx.Done():
		return ctx.Err()
	case b.jobs <- job:
	}

	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *writeBatcher) Close() {
	if b == nil {
		return
	}
	select {
	case <-b.stop:
		return
	default:
		close(b.stop)
		<-b.done
	}
}

func (b *writeBatcher) run() {
	defer close(b.done)

	var (
		pending []writeBatchJob
		timer   *time.Timer
		timerC  <-chan time.Time
	)

	resetTimer := func() {
		if timer == nil {
			timer = time.NewTimer(b.flushInterval)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(b.flushInterval)
		}
		timerC = timer.C
	}

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
	}

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		results := b.flush(pending)
		for i, job := range pending {
			job.result <- results[i]
			close(job.result)
		}
		pending = nil
		stopTimer()
	}

	for {
		select {
		case <-b.stop:
			for {
				select {
				case job := <-b.jobs:
					pending = append(pending, job)
				default:
					flushPending()
					return
				}
			}
		case <-timerC:
			flushPending()
		case job := <-b.jobs:
			pending = append(pending, job)
			if len(pending) == 1 {
				resetTimer()
			}
			if len(pending) >= b.maxBatchSize {
				flushPending()
			}
		}
	}
}

func (b *writeBatcher) flush(batch []writeBatchJob) []error {
	results := make([]error, len(batch))

	tx, err := b.db.BeginTx(context.Background(), nil)
	if err != nil {
		for i := range results {
			results[i] = err
		}
		return results
	}

	for i, job := range batch {
		select {
		case <-job.ctx.Done():
			results[i] = job.ctx.Err()
			continue
		default:
		}

		if _, err := tx.ExecContext(job.ctx, job.query, job.args...); err != nil {
			_ = tx.Rollback()
			for j := range results {
				if results[j] == nil {
					results[j] = err
				}
			}
			return results
		}
	}

	if err := tx.Commit(); err != nil {
		for i := range results {
			if results[i] == nil {
				results[i] = err
			}
		}
	}
	return results
}
