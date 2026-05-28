package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const (
	defaultJSONBodyLimit = 64 << 10
	largeJSONBodyLimit   = 1 << 20
)

var errRequestBodyTooLarge = errors.New("request body too large")

func decodeLimitedJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	return decodeLimitedJSONInternal(w, r, dst, limit, false)
}

func decodeOptionalLimitedJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	return decodeLimitedJSONInternal(w, r, dst, limit, true)
}

func decodeLimitedJSONInternal(w http.ResponseWriter, r *http.Request, dst any, limit int64, allowEmpty bool) error {
	if limit <= 0 {
		limit = defaultJSONBodyLimit
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := dec.Decode(dst); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		if isMaxBytesError(err) {
			return errRequestBodyTooLarge
		}
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if isMaxBytesError(err) {
			return errRequestBodyTooLarge
		}
		if err == nil {
			return errors.New("request body must contain a single JSON value")
		}
		return err
	}
	return nil
}

func isMaxBytesError(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func writeJSONDecodeError(w http.ResponseWriter, err error, code string, message string) {
	if errors.Is(err, errRequestBodyTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, code, message)
}
