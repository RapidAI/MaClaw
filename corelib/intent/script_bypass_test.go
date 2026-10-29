package intent

import "testing"

func TestIsPredominantlyCyrillic(t *testing.T) {
	cases := map[string]bool{
		"Серверийн төлөвийг шалгана уу":      true,
		"api2 серверийн төлөвийг шалга":      true,
		"Надад имэйл илгээ: report.pdf":      true,
		"查询服务器状态":                            false,
		"查詢伺服器狀態":                            false,
		"check the server status":            false,
		"帮我把 Привет 翻译成中文":                   false,
		"translate Привет to English please": false,
		"Я":        false,
		"":         false,
		"12345 !!": false,
		"ssh руу нэвтэрч логийг харуул": true,
	}
	for in, want := range cases {
		if got := IsPredominantlyCyrillic(in); got != want {
			t.Errorf("IsPredominantlyCyrillic(%q)=%v want %v", in, got, want)
		}
	}
}
