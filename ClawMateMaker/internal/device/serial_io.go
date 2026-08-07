package device

import (
	"errors"
	"io"
	"strings"

	"go.bug.st/serial"
)

// ReadBoundedLine reads one serial frame without bufio's speculative
// read-ahead. The application protocol is line-delimited and capped.
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
			if len(buf) > limit {
				return "", errors.New("serial frame exceeds size limit")
			}
			if one[0] == '\n' {
				return string(buf), nil
			}
		}
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

// IsSerialReadTimeout recognizes only retryable idle reads; disconnects and
// permission failures still terminate the probe.
func IsSerialReadTimeout(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "timeout") || strings.Contains(text, "timed out") || strings.Contains(text, "i/o timeout")
}
