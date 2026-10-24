package main

import (
	"strings"
	"testing"
)

func TestCorrectDesktopPathClaimQuietForNonScreenshotPaths(t *testing.T) {
	got := correctDesktopPathClaim("已发布：https://zhuanlan.zhihu.com/p/1\n配图原件在 /home/desktop/Desktop/x/fig1.jpg", "")
	if strings.Contains(got, "截图没有写到桌面") || strings.Contains(got, "/Desktop/") || !strings.Contains(got, "p/1") {
		t.Fatalf("got %q", got)
	}
}
