package tenant

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	adminJSONBodyLimit = 1 << 16
	cloudJSONBodyLimit = 1 << 20
)

var (
	errJSONBodyTooLarge = errors.New("json body exceeds size limit")
	errJSONTrailingData = errors.New("json body contains trailing data")
)

func decodeLimitedJSON(r io.Reader, v any, limit int64, allowEmpty bool) error {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return errJSONBodyTooLarge
	}
	if len(bytes.TrimSpace(body)) == 0 {
		if allowEmpty {
			return nil
		}
		return io.EOF
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errJSONTrailingData
		}
		return err
	}
	return nil
}
