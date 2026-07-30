package remote

import (
	"bytes"
	"io"
	"testing"
)

func TestCopyWithProgressCopiesAllBytesAndReportsChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 300*1024)
	var dst bytes.Buffer
	var progressed int64
	n, err := copyWithProgress(&dst, bytes.NewReader(payload), func(delta int64) { progressed += delta })
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || progressed != n || !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("copy n=%d progress=%d bytes=%d, want %d", n, progressed, dst.Len(), len(payload))
	}
}

func TestLimitedReaderAllowsExactLimitAndDetectsOverflow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		payload  int
		limit    int64
		wantRead int64
	}{
		{name: "exact limit", payload: 64, limit: 64, wantRead: 64},
		{name: "overflow", payload: 65, limit: 64, wantRead: 65},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := io.LimitReader(bytes.NewReader(bytes.Repeat([]byte("x"), tc.payload)), tc.limit+1)
			n, err := io.Copy(io.Discard, reader)
			if err != nil || n != tc.wantRead {
				t.Fatalf("read n=%d err=%v, want %d", n, err, tc.wantRead)
			}
			if overflow := n > tc.limit; overflow != (tc.payload > int(tc.limit)) {
				t.Fatalf("overflow=%v for payload=%d limit=%d", overflow, tc.payload, tc.limit)
			}
		})
	}
}
