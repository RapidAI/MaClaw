package desktopbot

import "testing"

func TestActivityPhraseCarriesOnlyClosedSites(t *testing.T) {
	for _, ok := range []string{"正在登录 CSDN", "正在发布 小红书", "正在思考", "正在打开网页 知乎"} {
		if ActivityPhrase(ok) != ok {
			t.Fatalf("dropped %q", ok)
		}
	}
	for _, bad := range []string{"正在登录 evil.com", "正在登录 run_6fa834e0", "browser_click", "正在一点点推进", "正在登录 CSDN 账号"} {
		if ActivityPhrase(bad) != "" {
			t.Fatalf("kept %q", bad)
		}
	}
	if ActivitySiteForHost("passport.csdn.net") != "CSDN" || ActivitySiteForHost("notcsdn.net") != "" || ActivitySiteForHost("creator.xiaohongshu.com") != "小红书" {
		t.Fatal("host map")
	}
}
