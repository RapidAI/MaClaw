package guiapp

import (
	"log"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const dumpFrontendDOMJS = `(function(){
  try {
    var root = document.getElementById('root');
    var app = document.getElementById('App');
    var splash = document.getElementById('maclaw-boot-splash');
    var css = [];
    var ls = document.querySelectorAll('link[rel="stylesheet"]');
    for (var i = 0; i < ls.length; i++) {
      var n = -1;
      try { if (ls[i].sheet && ls[i].sheet.cssRules) n = ls[i].sheet.cssRules.length; } catch (e) {}
      css.push((ls[i].media||'') + ':' + n);
    }
    var text = ((root && root.innerText) || '').replace(/\s+/g, ' ').slice(0, 160);
    var q = 'stage=dom&root=' + (root ? root.childElementCount : -1) +
      '&rootH=' + (root ? root.offsetHeight : -1) +
      '&app=' + (app ? '1' : '0') +
      '&appH=' + (app ? app.offsetHeight : 0) +
      '&splash=' + (splash ? '1' : '0') +
      '&css=' + encodeURIComponent(css.join(',')) +
      '&go=' + ((window.go && window.go.main && window.go.main.App) ? '1' : '0') +
      '&rt=' + (typeof window.runtime) +
      '&wails=' + (typeof window.wails) +
      '&gok=' + encodeURIComponent(window.go ? Object.keys(window.go).join('.') : '') +
      '&err=' + encodeURIComponent(String(window.__MACLAW_LAST_ERROR || '')) +
      '&text=' + encodeURIComponent(text);
    fetch('/maclaw-boot/ping?' + q);
  } catch (e) {
    try { fetch('/maclaw-boot/ping?stage=dom&err=' + encodeURIComponent(String(e))); } catch (e2) {}
  }
})();`

const promotePastEnvCheckSplashJS = `(function(){
  try { if (window.go && window.go.guiapp && !window.go.main) window.go.main = window.go.guiapp; } catch (e) {}
  window.__MACLAW_ENV_CHECK_DONE = true;
  try { window.dispatchEvent(new Event('maclaw-env-check-done')); } catch (e) {}
  var ls = document.querySelectorAll('link[rel="stylesheet"]');
  for (var i = 0; i < ls.length; i++) { ls[i].media = 'all'; }
  function hide() {
    var splash = document.getElementById('maclaw-boot-splash');
    if (!splash) return true;
    if (document.getElementById('App')) { splash.remove(); return true; }
    return false;
  }
  if (hide()) return;
  var n = 0;
  var t = setInterval(function(){
    n++;
    if (hide() || n >= 80) clearInterval(t);
  }, 100);
})();`

// promotePastEnvCheckSplash leaves the compact HTML splash even if React missed
// the Wails env-check-done event (it often fires before App's useEffect subscribes).
func (a *App) promotePastEnvCheckSplash(reason string) {
	if a == nil || !a.frontendEnvCheckPromoted.CompareAndSwap(false, true) {
		return
	}
	bootLog("promote past env-check splash reason=%s reactReady=%v", reason, a.frontendReactReady.Load())
	if a.ctx == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			bootLog("promote past env-check splash panic %v", r)
		}
	}()
	runtime.WindowExecJS(a.ctx, promotePastEnvCheckSplashJS)
	runtime.WindowShow(a.ctx)
	go func() {
		time.Sleep(800 * time.Millisecond)
		size := a.GetAdaptiveWindowSize()
		a.ResizeWindow(size["width"], size["height"])
		syncWebViewClientSize()
		bootLog("delayed ResizeWindow after wails bind %dx%d", size["width"], size["height"])
		time.Sleep(700 * time.Millisecond)
		runtime.WindowExecJS(a.ctx, dumpFrontendDOMJS)
		captureMainWindowPNG("t1.5s")
		syncWebViewClientSize()
		time.Sleep(2500 * time.Millisecond)
		runtime.WindowExecJS(a.ctx, dumpFrontendDOMJS)
		captureMainWindowPNG("t4s")
		syncWebViewClientSize()
	}()
}

func (a *App) watchEnvCheckPromote() {
	if a == nil {
		return
	}
	go func() {
		for i := 0; i < 40; i++ {
			time.Sleep(200 * time.Millisecond)
			if a.frontendEnvCheckPromoted.Load() {
				return
			}
			if a.frontendEnvCheckDone.Load() {
				a.promotePastEnvCheckSplash("watchdog-done")
				return
			}
		}
		bootLog("env-check promote watchdog timeout react=%v done=%v", a.frontendReactReady.Load(), a.frontendEnvCheckDone.Load())
		a.promotePastEnvCheckSplash("watchdog-timeout")
	}()
}

// watchFrontendPaint recovers a blank about:blank WebView (no asset requests,
// no OnDomReady) by injecting the env-check splash and retrying navigation.
func (a *App) watchFrontendPaint() {
	if a == nil {
		return
	}
	go func() {
		bootLog("watchFrontendPaint goroutine start ctx_nil=%v", a.ctx == nil)
		recoverBlankWebView("immediate")
		time.Sleep(1500 * time.Millisecond)
		bootLog("watch 1.5s tick navReady=%v htmlReady=%v", a.frontendDOMReady.Load(), a.frontendHTMLReady.Load())
		if a.frontendHTMLReady.Load() {
			return
		}
		log.Printf("[startup] WebView DOM not ready after 1.5s err=dom-timeout; injecting boot splash")
		recoverBlankWebView("1.5s")
		w, h := envCheckWindowSize()
		bootLog("WindowShow/SetSize %dx%d", w, h)
		runtime.WindowShow(a.ctx)
		runtime.WindowSetSize(a.ctx, w+2, h+2)
		runtime.WindowSetSize(a.ctx, w, h)
		bootLog("WindowExecJS inject splash")
		runtime.WindowExecJS(a.ctx, frontendBootSplashInjectJS)
		time.Sleep(3 * time.Second)
		bootLog("watch 4.5s tick navReady=%v htmlReady=%v", a.frontendDOMReady.Load(), a.frontendHTMLReady.Load())
		if a.frontendHTMLReady.Load() {
			return
		}
		log.Printf("[startup] WebView still not ready err=dom-timeout; reloading asset host")
		runtime.WindowExecJS(a.ctx, "window.location.replace('http://wails.localhost/');")
		bootLog("forced location.replace http://wails.localhost/")
	}()
}

const frontendBootSplashInjectJS = `(function(){if(document.getElementById('maclaw-boot-splash'))return;try{document.body.style.margin='0';document.body.style.background='#f3f5f7';document.body.innerHTML='<div id="maclaw-boot-splash" style="position:fixed;inset:0;display:flex;align-items:center;justify-content:center;font-family:Segoe UI,sans-serif;background:#f3f5f7;color:#1c1c1e"><div style="width:min(400px,92%);padding:24px 28px;border-radius:28px;background:#fff;box-shadow:0 18px 48px rgba(15,23,42,.08);text-align:center"><h1 style="margin:8px 0 6px;font-size:1.4rem">正在准备环境</h1><p style="margin:0;color:#8e8e93;font-size:.86rem">正在准备运行环境，请稍候</p></div></div>';}catch(e){}})();`
