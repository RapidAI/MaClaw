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
		flushInterval = 100 * time.Millisecond
	}

	maxBatchSize := cfg.BatchMaxSize
	if maxBatchSize <= 0 {
		maxBatchSize = 128
	}

	queueSize := cfg.BatchQueueSize
	if queueSize <= 0 {
		queueSize = 4096
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

	allOK := true
	failIdx := -1
	for i, job := range batch {
		select {
		case <-job.ctx.Done():
			results[i] = job.ctx.Err()
			continue
		default:
		}

		if _, err := tx.ExecContext(job.ctx, job.query, job.args...); err != nil {
			results[i] = err
			allOK = false
			failIdx = i
			break
		}
	}

	if allOK {
		if err := tx.Commit(); err != nil {
			for i := range results {
				if results[i] == nil {
					results[i] = err
				}
			}
		}
		return results
	}

	// Batch failed — rollback and retry every job individually.
	// Each job gets its own implicit transaction. This is slower than batch mode
	// but guarantees no collateral damage: each job either succeeds or gets its
	// own specific error. This path is only triggered when one job has bad SQL/data,
	// which is rare in production.
	_ = tx.Rollback()
	for i, job := range batch {
		if i == failIdx {
			continue // already has error from batch attempt
		}
		if results[i] != nil {
			continue // already has ctx.Err()
		}
		select {
		case <-job.ctx.Done():
			results[i] = job.ctx.Err()
			continue
		default:
		}
		if _, execErr := b.db.ExecContext(job.ctx, job.query, job.args...); execErr != nil {
			results[i] = execErr
		}
	}
	return results
}
