package commands

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestCaptureOutputUsesPackageStdout(t *testing.T) {
	out, err := CaptureOutput(func() error {
		_, _ = Println("hello-capture")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello-capture") {
		t.Fatalf("out=%q", out)
	}
}

func TestCaptureOutputDoesNotRedirectOsStdout(t *testing.T) {
	// Root fix: process os.Stdout must stay untouched so concurrent logs are not swallowed.
	old := os.Stdout
	out, err := CaptureOutput(func() error {
		// Process-wide stdout must NOT be captured.
		_, _ = fmt.Fprintln(os.Stdout, "os-stdout-leak")
		// Package CLI stdout is captured.
		_, _ = Println("pkg-stdout")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if os.Stdout != old {
		t.Fatal("CaptureOutput must not replace os.Stdout")
	}
	if strings.Contains(out, "os-stdout-leak") {
		t.Fatalf("must not capture process os.Stdout: %q", out)
	}
	if !strings.Contains(out, "pkg-stdout") {
		t.Fatalf("expected package stdout, out=%q", out)
	}
}

func TestCaptureOutputRecoversPanic(t *testing.T) {
	_, err := CaptureOutput(func() error {
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err=%v", err)
	}
	// Stdio restored after panic.
	_, _ = Println("after-panic")
}

func TestCaptureOutputPropagatesError(t *testing.T) {
	out, err := CaptureOutput(func() error {
		_, _ = Println("partial")
		return fmt.Errorf("failed")
	})
	if err == nil || err.Error() != "failed" {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out, "partial") {
		t.Fatalf("out=%q", out)
	}
}

func TestCaptureOutputIncludesStderr(t *testing.T) {
	out, err := CaptureOutput(func() error {
		_, _ = Eprintln("warn-from-stderr")
		_, _ = Println("from-stdout")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "from-stdout") || !strings.Contains(out, "warn-from-stderr") {
		t.Fatalf("out=%q", out)
	}
}

func TestTruncateCapture(t *testing.T) {
	small := "hello"
	if got := truncateCapture(small); got != small {
		t.Fatalf("got %q", got)
	}
	big := strings.Repeat("a", maxCaptureBytes+100)
	got := truncateCapture(big)
	if !strings.HasSuffix(got, captureTruncationMarker) {
		t.Fatalf("missing truncation marker: %q", got[len(got)-40:])
	}
	if len(got) > maxCaptureBytes+len(captureTruncationMarker) {
		t.Fatalf("still too large: %d", len(got))
	}
}

func TestCaptureOutputCapsLargeStdout(t *testing.T) {
	huge := strings.Repeat("x", maxCaptureBytes+8192)
	out, err := CaptureOutput(func() error {
		_, _ = Print(huge)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, captureTruncationMarker) {
		t.Fatalf("expected truncation marker, len=%d", len(out))
	}
	if len(out) > maxCaptureBytes+len(captureTruncationMarker)+8 {
		t.Fatalf("output still too large: %d", len(out))
	}
	if !strings.HasPrefix(out, "xxxx") {
		t.Fatalf("expected head retained, got prefix %q", out[:min(16, len(out))])
	}
}

func TestCappedWriterAcceptsAfterFull(t *testing.T) {
	c := &cappedWriter{buf: make([]byte, 0, 8), max: 8}
	n, err := c.Write([]byte("abcdefgh"))
	if err != nil || n != 8 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err = c.Write([]byte("MORE"))
	if err != nil || n != 4 {
		t.Fatalf("discard write n=%d err=%v", n, err)
	}
	if !c.truncated {
		t.Fatal("expected truncated")
	}
	if got := c.String(); !strings.HasSuffix(got, captureTruncationMarker) {
		t.Fatalf("got %q", got)
	}
}

func TestCaptureOutputSerializesConcurrentCalls(t *testing.T) {
	const n = 8
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			label := fmt.Sprintf("cap-%d", i)
			out, err := CaptureOutput(func() error {
				_, _ = Println(label)
				return nil
			})
			if err != nil {
				errCh <- err
				return
			}
			if !strings.Contains(out, label) {
				errCh <- fmt.Errorf("missing %q in %q", label, out)
				return
			}
			// Concurrent process logs must never appear in capture.
			if strings.Contains(out, "os-stdout-leak") {
				errCh <- fmt.Errorf("unexpected process leak in %q", out)
				return
			}
			errCh <- nil
		}()
	}
	// Background noise on process stdout while captures run.
	go func() {
		for i := 0; i < 20; i++ {
			_, _ = fmt.Fprintln(os.Stdout, "os-stdout-leak")
		}
	}()
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSkillListFlagErrorDoesNotExit(t *testing.T) {
	// ContinueOnError: unknown flags return error instead of os.Exit.
	err := skillList([]string{"--not-a-real-flag"})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSkillListCapturedViaPackageStdout(t *testing.T) {
	// End-to-end: real install CLI path uses package writers under CaptureOutput.
	out, err := RunSkillCaptured([]string{"list"})
	if err != nil {
		// empty skills dir is fine; only hard failures matter
		t.Logf("list err (may be ok): %v", err)
	}
	// Either empty list message or table header — both go through package stdout.
	if out == "" && err == nil {
		t.Fatal("expected some captured output")
	}
}

func TestPrintHelpersHoldLockAcrossWrite(t *testing.T) {
	// Concurrent Println during CaptureOutput must not panic or drop package output.
	const n = 32
	errCh := make(chan error, n+1)
	go func() {
		out, err := CaptureOutput(func() error {
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					_, _ = Printf("line-%d\n", i)
				}(i)
			}
			wg.Wait()
			return nil
		})
		if err != nil {
			errCh <- err
			return
		}
		for i := 0; i < n; i++ {
			if !strings.Contains(out, fmt.Sprintf("line-%d", i)) {
				errCh <- fmt.Errorf("missing line-%d in %q", i, out)
				return
			}
		}
		errCh <- nil
	}()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}
