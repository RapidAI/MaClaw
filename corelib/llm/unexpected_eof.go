package llm

import (
	"errors"
	"io"
)

// IsUnexpectedStreamEOF reports a body read that stopped inside a frame.
// net/http returns io.ErrUnexpectedEOF for a truncated chunked or HTTP/2 body,
// and the SSE readers keep that sentinel with %w. A clean io.EOF is the normal
// end of a finished body. The same words in a copied string are not this error:
// encoding/json reports a short document as "unexpected end of JSON input".
func IsUnexpectedStreamEOF(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF)
}
