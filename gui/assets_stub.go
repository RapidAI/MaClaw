//go:build !desktop && !production

package main

import "testing/fstest"

var assets = fstest.MapFS{
	"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html><body>test assets</body></html>")},
}
