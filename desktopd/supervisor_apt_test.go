package desktopd

import (
	"strings"
	"testing"
)

// aptRootPrelude builds a container filesystem under tmp/rootfs whose
// debian.sources is the one images built on Tencent Cloud carried, and a fake
// probe with the timings measured from the Tencent Cloud host through the
// egress proxy (the intranet mirror is not a candidate at all).
const aptRootPrelude = `
root = tmp / "rootfs"
for d in ("etc/apt/sources.list.d", "etc/apt/apt.conf.d", "etc/sudoers.d"):
    (root / d).mkdir(parents=True)
(root / "etc/os-release").write_text('PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"\nVERSION_CODENAME=bookworm\n')
stock = "Types: deb\nURIs: http://mirrors.tencentyun.com/debian\nSuites: bookworm bookworm-updates\nComponents: main\n\nTypes: deb\nURIs: http://mirrors.tencentyun.com/debian-security\nSuites: bookworm-security\nComponents: main\n"
(root / sup.APT_SOURCES).write_text(stock)
timings = {}
def probe(mirror, codename, proxy):
    return timings.get((mirror, bool(proxy)))
proxy_env = {"HTTP_PROXY": "http://172.17.0.1:18083", "http_proxy": "http://172.17.0.1:18083", "NO_PROXY": "localhost"}
def uris():
    return [l.split()[1] for l in (root / sup.APT_SOURCES).read_text().splitlines() if l.startswith("URIs:")]
def proxy_conf():
    p = root / sup.APT_PROXY_CONF
    return p.read_text().strip().splitlines()[1:] if p.exists() else None
`

func TestAptLeavesTheIntranetMirrorAndBypassesTheProxyForTheFasterMirror(t *testing.T) {
	out := runSupervisorPython(t, aptRootPrelude+`
timings.update({("http://deb.debian.org", False): 1.4, ("http://deb.debian.org", True): 0.8,
                ("https://mirrors.tencent.com", False): 0.2, ("https://mirrors.tencent.com", True): None})
r = sup.configure_apt(root, proxy_env, probe, now=1000)
print(r["mirror"], r["via_proxy"])
print(uris())
print(proxy_conf())
print((root / sup.APT_SUDOERS).read_text().splitlines()[1])
print(oct((root / sup.APT_SUDOERS).stat().st_mode & 0o777))
print(sup.apt_config_current(root, proxy_env, now=2000), sup.apt_config_current(root, {}, now=2000), sup.apt_config_current(root, proxy_env, now=1000 + 2 * 86400))
`)
	want := strings.Join([]string{
		"https://mirrors.tencent.com False",
		"['https://mirrors.tencent.com/debian', 'https://mirrors.tencent.com/debian-security']",
		`['Acquire::http::Proxy "http://172.17.0.1:18083";', 'Acquire::https::Proxy "http://172.17.0.1:18083";', 'Acquire::http::Proxy::mirrors.tencent.com "DIRECT";', 'Acquire::https::Proxy::mirrors.tencent.com "DIRECT";']`,
		`Defaults env_keep += "http_proxy https_proxy no_proxy HTTP_PROXY HTTPS_PROXY NO_PROXY"`,
		"0o440",
		"True False False",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestAptGoesThroughTheProxyWhenThatIsFasterAndDropsItWithoutOne(t *testing.T) {
	out := runSupervisorPython(t, aptRootPrelude+`
timings.update({("http://deb.debian.org", False): None, ("http://deb.debian.org", True): 0.05,
                ("https://mirrors.tencent.com", False): None, ("https://mirrors.tencent.com", True): 0.3})
r = sup.configure_apt(root, proxy_env, probe, now=1000)
print(r["mirror"], r["via_proxy"], uris()[0])
print(proxy_conf())
# Egress proxy disabled later: the apt proxy file goes, the mirror is measured again.
timings.update({("http://deb.debian.org", False): 0.05, ("https://mirrors.tencent.com", False): 0.25})
r = sup.configure_apt(root, {"HTTP_PROXY": ""}, probe, now=2000)
print(r["mirror"], r["via_proxy"], proxy_conf())
# Nothing answers: Debian's own mirror through whatever route there is.
timings.clear()
print(sup.configure_apt(root, proxy_env, probe, now=3000)["mirror"], proxy_conf()[-1])
`)
	want := strings.Join([]string{
		"http://deb.debian.org True http://deb.debian.org/debian",
		`['Acquire::http::Proxy "http://172.17.0.1:18083";', 'Acquire::https::Proxy "http://172.17.0.1:18083";']`,
		"http://deb.debian.org False None",
		`http://deb.debian.org Acquire::https::Proxy "http://172.17.0.1:18083";`,
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestAptMirrorSettingAndPersonalSources(t *testing.T) {
	out := runSupervisorPython(t, aptRootPrelude+`
for raw in ("", "auto", "off", "mirrors.ustc.edu.cn", "https://mirrors.tencent.com/debian/", "a.example, b.example", "ftp://x", 'http://x/";'):
    print(repr(raw), sup.apt_mirror_setting({"MACLAW_APT_MIRROR": raw}))
# A fixed mirror without a proxy is used without measuring anything.
r = sup.configure_apt(root, {"MACLAW_APT_MIRROR": "mirrors.ustc.edu.cn"}, None, now=1)
print(uris()[0], proxy_conf())
# Sources a person pointed elsewhere stay; apt still gets the proxy.
(root / sup.APT_SOURCES).write_text("Types: deb\nURIs: http://mirrors.aliyun.com/debian\n")
r = sup.configure_apt(root, proxy_env, probe, now=2)
print(r["mirror"] == "", uris(), len(proxy_conf()))
# off keeps even the stock intranet file.
(root / sup.APT_SOURCES).write_text(stock)
sup.configure_apt(root, dict(proxy_env, MACLAW_APT_MIRROR="off"), probe, now=3)
print(uris()[0])
# Image build: deb.debian.org, no decision recorded, no proxy baked in.
(root / sup.APT_PROXY_CONF).unlink()
(root / sup.APT_STATE).unlink()
sup.configure_apt(root, proxy_env, probe, default_only=True)
print(uris(), proxy_conf(), (root / sup.APT_STATE).exists(), sup.apt_config_current(root, {}))
print(sup.apt_sources_replaceable("deb http://deb.debian.org/debian bookworm main\ndeb http://security.debian.org/debian-security bookworm-security main\n"))
`)
	want := strings.Join([]string{
		`'' ('auto', ['http://deb.debian.org', 'https://mirrors.tencent.com'])`,
		`'auto' ('auto', ['http://deb.debian.org', 'https://mirrors.tencent.com'])`,
		`'off' ('off', [])`,
		`'mirrors.ustc.edu.cn' ('fixed', ['http://mirrors.ustc.edu.cn'])`,
		`'https://mirrors.tencent.com/debian/' ('fixed', ['https://mirrors.tencent.com'])`,
		`'a.example, b.example' ('auto', ['http://a.example', 'http://b.example'])`,
		`'ftp://x' ('auto', ['http://deb.debian.org', 'https://mirrors.tencent.com'])`,
		`'http://x/";' ('auto', ['http://deb.debian.org', 'https://mirrors.tencent.com'])`,
		"http://mirrors.ustc.edu.cn/debian None",
		"True ['http://mirrors.aliyun.com/debian'] 2",
		"http://mirrors.tencentyun.com/debian",
		"['http://deb.debian.org/debian', 'http://deb.debian.org/debian-security'] None False False",
		"True",
	}, "\n")
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}
