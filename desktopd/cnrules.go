package desktopd

import (
	"net"
	"strings"
)

// cnDirectSuffixes are mainland-China sites a desktop in "smart" proxy mode
// reaches directly from this host instead of through the overseas upstream.
// Routing through the relay makes these sites see a foreign exit IP: jd.com
// then redirects to its global store, and several of them throttle or block
// the relay outright. Matching is by domain suffix only, so no local DNS
// lookup is needed to decide (foreign names never get resolved here).
var cnDirectSuffixes = []string{
	// TLDs that are domestic by definition.
	"cn", "xn--fiqs8s", // .cn, .中国
	// E-commerce.
	"jd.com", "jd.hk", "360buyimg.com", "jdcloud.com", "jingxi.com",
	"taobao.com", "tmall.com", "alicdn.com", "alipay.com", "alipayobjects.com",
	"aliyun.com", "alibaba.com", "1688.com", "tbcdn.cn", "mmstat.com",
	"pinduoduo.com", "yangkeduo.com", "pddpic.com", "suning.com", "vip.com",
	"dangdang.com", "meituan.com", "meituan.net", "dianping.com", "ele.me",
	// Search, portals, news.
	"baidu.com", "bdstatic.com", "bdimg.com", "baidupcs.com", "sogou.com",
	"so.com", "360.cn", "sina.com.cn", "sinaimg.cn", "weibo.com", "weibo.cn",
	"sohu.com", "163.com", "126.net", "netease.com", "ifeng.com", "toutiao.com",
	"qq.com", "gtimg.com", "qpic.cn", "weixin.qq.com", "wechat.com", "tencent.com",
	"myqcloud.com", "people.com.cn", "xinhuanet.com", "thepaper.cn",
	// Video, social, content.
	"bilibili.com", "hdslb.com", "biliapi.net", "douyin.com", "douyinpic.com",
	"bytedance.com", "byteimg.com", "zijieapi.com", "ixigua.com", "kuaishou.com",
	"iqiyi.com", "youku.com", "mgtv.com", "zhihu.com", "zhimg.com", "douban.com",
	"doubanio.com", "xiaohongshu.com", "xhscdn.com", "csdn.net", "jianshu.com",
	"cnblogs.com", "gitee.com", "oschina.net", "juejin.cn",
	// Travel, maps, payments, misc.
	"ctrip.com", "trip.com", "12306.cn", "amap.com", "autonavi.com",
	"didiglobal.com", "xiaojukeji.com", "unionpay.com", "95516.com",
	"huawei.com", "hicloud.com", "mi.com", "xiaomi.com", "oppo.com", "vivo.com.cn",
	"tsinghua.edu.cn", "ustc.edu.cn",
}

// cnDirect is the normalized form of cnDirectSuffixes.
var cnDirect = normalizeDirectSuffixes(cnDirectSuffixes)

// hostOnSuffixList reports whether authority (host or host:port) equals or is
// a subdomain of one of the normalized suffixes.
func hostOnSuffixList(authority string, suffixes []string) bool {
	if authority == "" || len(suffixes) == 0 {
		return false
	}
	host := authority
	if h, _, err := net.SplitHostPort(authority); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	host = strings.Trim(host, ".")
	if host == "" {
		return false
	}
	for _, suffix := range suffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// cnTarget reports whether authority is on the built-in mainland list.
func cnTarget(authority string) bool { return hostOnSuffixList(authority, cnDirect) }
