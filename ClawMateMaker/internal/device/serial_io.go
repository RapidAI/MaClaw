package device

import (
	"errors"
	"io"
	"strings"

	"go.bug.st/serial"
)

// readBoundedLine reads one serial frame without bufio's speculative
// read-ahead. The application protocol is line-delimited and capped, so this
// keeps one timed Read bound to one operation and prevents a cancelled probe
// from stealing a future nonce response.
func ReadBoundedLine(port serial.Port, limit int) (string, error) {
	if limit <= 0 {
		return "", errors.New("invalid serial frame limit")
	}
	buf := make([]byte, 0, limit)
	one := make([]byte, 1)
	for len(buf) <= limit {
		n, err := port.Read(one)
		if n > 0 {
			buf = append(buf, one[:n]...)
			if one[0] == '\n' {
				return string(buf), nil
			}
			if len(buf) > limit {
				return "", errors.New("serial frame exceeds size limit")
			}
		}
		// go.bug.st/serial reports a configured idle timeout as (0, nil)
		// on Unix (and normally on Windows through COMMTIMEOUTS). Surface a
		// stable timeout error to callers rather than spinning forever here.
		if n == 0 && err == nil {
			return string(buf), errSerialReadTimeout
		}
		if err != nil {
			return string(buf), err
		}
	}
	return "", errors.New("serial frame exceeds size limit")
}

var errSerialReadTimeout = errors.New("serial read timeout")

// Serial backends use platform-specific timeout errors. Treat only the known
// no-byte/timeout forms as a retryable idle read; all other errors still end
// the probe to avoid hiding disconnects or permission loss.
func IsSerialReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "timeout") || strings.Contains(text, "timed out") || strings.Contains(text, "i/o timeout")
}
