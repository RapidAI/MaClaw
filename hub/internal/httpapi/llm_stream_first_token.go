package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// First-token failover for streamed chat. An auto-routed request whose
// provider sends nothing (no headers, or headers and then silence) used to
// wait on that provider until the client gave up: 10-22 21:30 a desktop
// client got 200 and 80 s of Hub keepalive comments, no model data, while a
// second provider was configured. When another provider remains, Hub now
// gives each attempt MACLAW_LLM_FIRST_TOKEN_TIMEOUT_SEC (default 25, 0 = off)
// to deliver its first data line before anything is written to the client,
// then closes it and tries the next one. Every streamed attempt logs the
// provider, the first-data wait and the outcome.

func streamFirstTokenTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("MACLAW_LLM_FIRST_TOKEN_TIMEOUT_SEC"))
	if v == "" {
		return 25 * time.Second
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 25 * time.Second
	}
	return time.Duration(f * float64(time.Second))
}

type streamFirstTokenGuard struct {
	started  time.Time
	limit    time.Duration
	failover bool
	ctx      context.Context
	cancel   context.CancelFunc
	timer    *time.Timer
	fired    chan struct{}
}

// newStreamFirstTokenGuard returns the request to send to one provider. With
// failover the attempt is cancelled when no first data arrives in time.
func newStreamFirstTokenGuard(r *http.Request, failover bool) (*http.Request, *streamFirstTokenGuard) {
	ctx, cancel := context.WithCancel(r.Context())
	g := &streamFirstTokenGuard{started: time.Now(), limit: streamFirstTokenTimeout(), failover: failover, ctx: ctx, cancel: cancel, fired: make(chan struct{})}
	if failover && g.limit > 0 {
		g.timer = time.AfterFunc(g.limit, func() {
			close(g.fired)
			cancel()
		})
	}
	return r.WithContext(ctx), g
}

func (g *streamFirstTokenGuard) timedOut() bool {
	select {
	case <-g.fired:
		return true
	default:
		return false
	}
}

type prefixedReadCloser struct {
	io.Reader
	io.Closer
}

// awaitFirstData reads until the first non-comment line, keeps what it read
// in front of resp.Body, and reports a provider that stayed silent.
func (g *streamFirstTokenGuard) awaitFirstData(resp *http.Response, providerID string) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	if !g.failover || g.limit <= 0 || resp.StatusCode >= 400 {
		log.Printf("[LLM-V1] stream provider=%s status=%d headers_ms=%d failover=off", providerID, resp.StatusCode, time.Since(g.started).Milliseconds())
		return nil
	}
	br := bufio.NewReaderSize(resp.Body, 4096)
	var got bytes.Buffer
	for {
		line, err := br.ReadBytes('\n')
		got.Write(line)
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && trimmed[0] != ':' {
			break
		}
		if err != nil {
			if g.timedOut() {
				_ = resp.Body.Close()
				log.Printf("[LLM-V1] stream provider=%s outcome=no_first_token wait_ms=%d; trying next provider", providerID, time.Since(g.started).Milliseconds())
				return fmt.Errorf("provider %q sent no data within %s", providerID, g.limit)
			}
			if len(bytes.TrimSpace(got.Bytes())) > 0 || err == io.EOF {
				break
			}
			_ = resp.Body.Close()
			return err
		}
	}
	if g.timer != nil && !g.timer.Stop() && g.timedOut() {
		_ = resp.Body.Close()
		log.Printf("[LLM-V1] stream provider=%s outcome=no_first_token wait_ms=%d; trying next provider", providerID, time.Since(g.started).Milliseconds())
		return fmt.Errorf("provider %q sent no data within %s", providerID, g.limit)
	}
	log.Printf("[LLM-V1] stream provider=%s status=%d first_data_ms=%d outcome=streaming", providerID, resp.StatusCode, time.Since(g.started).Milliseconds())
	resp.Body = prefixedReadCloser{Reader: io.MultiReader(bytes.NewReader(got.Bytes()), br), Closer: resp.Body}
	return nil
}
