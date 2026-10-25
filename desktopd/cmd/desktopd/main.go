package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/desktopd"
)

func main() {
	// Open the rotating log before anything else so a bad env value is recorded
	// in the file as well as on stderr. systemd 237 cannot do this for us.
	if err := desktopd.ConfigureProcessLog(); err != nil {
		log.Fatalf("[desktopd] log file: %v", err)
	}
	addr := strings.TrimSpace(os.Getenv("DESKTOPD_ADDR"))
	if addr == "" {
		addr = ":18081"
	}
	token := strings.TrimSpace(os.Getenv("DESKTOPD_TOKEN"))
	if token == "" {
		log.Fatal("DESKTOPD_TOKEN is unset. Write DESKTOPD_TOKEN=<token> in the deploy directory .env, then systemctl restart maclaw-desktopd.")
	}
	svc := &desktopd.Service{
		AdvertiseHost: strings.TrimSpace(os.Getenv("DESKTOPD_ADVERTISE_HOST")),
		ComputerDir:   desktopd.ComputerDirFromEnv(os.Getenv("DESKTOPD_COMPUTER_DIR")),
	}
	publishBind, err := desktopd.ParsePublishBind(os.Getenv("DESKTOPD_PUBLISH_BIND"))
	if err != nil {
		log.Fatalf("[desktopd] %v", err)
	}
	svc.PublishBind = publishBind
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_PUBLIC_URL")); raw != "" {
		canonical, err := desktopd.ParsePublicURL(raw)
		if err != nil {
			log.Fatalf("[desktopd] %v", err)
		}
		svc.PublicURL = canonical
	}
	// DESKTOPD_IMAGE_SOURCE: where a missing maclaw-gui:2 comes from. Empty
	// means the public ghcr.io/rapidai/maclaw-gui:2; "off" disables pulls
	// (the image is built on the host by remote_deploy.sh). A present local
	// image is never pulled over or replaced.
	sources, err := desktopd.ParseImageSources(os.Getenv("DESKTOPD_IMAGE_SOURCE"))
	if err != nil {
		log.Fatalf("DESKTOPD_IMAGE_SOURCE is invalid: %v", err)
	}
	svc.ImageSources = sources
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_IMAGE_PULL_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			log.Fatalf("DESKTOPD_IMAGE_PULL_TIMEOUT=%q is not a duration. Set it to 10m in .env and restart desktopd.", raw)
		}
		svc.ImagePullTimeout = timeout
	}
	// The admin panel keeps its account and extra API keys here. Defaults to
	// ./data, which resolves to the deploy directory under the systemd unit.
	// DESKTOPD_STATE_DIR=off disables the panel entirely.
	stateDir := strings.TrimSpace(os.Getenv("DESKTOPD_STATE_DIR"))
	adminDisabled := strings.EqualFold(stateDir, "off")
	if stateDir == "" {
		stateDir = "data"
	}
	// Restart always would undo an explicit docker stop. Refuse it at start.
	if err := desktopd.RejectRestartAlways(desktopd.RestartPolicy()); err != nil {
		log.Fatalf("[desktopd] %v", err)
	}
	// DESKTOPD_SLEEP is stage 2. Empty or off keeps containers running.
	// On: after about five minutes, flush, sync the four volumes, docker stop.
	// It replaces DESKTOPD_MAX_IDLE for that process so a failed sync is not
	// followed by the historical stop.
	stage2, err := desktopd.ParseStage2Sleep(os.Getenv("DESKTOPD_SLEEP"))
	if err != nil {
		log.Fatalf("[desktopd] %v", err)
	}
	svc.Stage2Sleep = stage2
	if svc.Stage2Sleep {
		if strings.TrimSpace(os.Getenv("DESKTOPD_MAX_IDLE")) != "" {
			log.Printf("[desktopd] DESKTOPD_SLEEP is on; DESKTOPD_MAX_IDLE is not used")
		}
		svc.StartReaper(context.Background(), 10*time.Minute, desktopd.Stage2Idle)
		log.Printf("[desktopd] stage2 sleep enabled idle=%s restart=%s", desktopd.Stage2Idle, desktopd.RestartPolicy())
	} else if raw := strings.TrimSpace(os.Getenv("DESKTOPD_MAX_IDLE")); raw != "" {
		// DESKTOPD_MAX_IDLE stops desktops idle longer than this, catching
		// containers Hub can no longer name (moved assignments, crashed callers).
		// Empty keeps the historical behaviour of never reaping.
		maxIdle, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("DESKTOPD_MAX_IDLE is not a duration. Set it to 30m, or leave it empty to disable reaping, then restart desktopd.")
		}
		if maxIdle > 0 {
			svc.StartReaper(context.Background(), 10*time.Minute, maxIdle)
			log.Printf("[desktopd] idle reaper enabled max_idle=%s", maxIdle)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The forward-proxy key resolves from the panel's file when a state
	// directory exists (DESKTOPD_PROXY_TOKEN / DESKTOPD_TOKEN are the
	// bootstrap fallback); the panel can replace it without a restart.
	proxyFallback := strings.TrimSpace(os.Getenv("DESKTOPD_PROXY_TOKEN"))
	if proxyFallback == "" {
		proxyFallback = token
	}
	keyStateDir := stateDir
	if adminDisabled {
		keyStateDir = ""
	}
	proxyKeys := desktopd.NewProxyKeySource(keyStateDir, proxyFallback)
	// DESKTOPD_DESKTOP_PROXY_URL points desktop containers at the forward
	// proxy so the browser inside egresses through it. The URL's host must be
	// a private IP (the docker bridge gateway, e.g. http://172.17.0.1:18083);
	// its unauthenticated listener is only bound there. When
	// DESKTOPD_UPSTREAM_PROXY is also set, the listener chains to that
	// (overseas) proxy instead of dialing targets directly. The panel's
	// egress config (egress_proxy.json) can repoint the chain and the
	// desktop env live; the env values are the bootstrap fallback.
	desktopCanonical := ""
	desktopBind := ""
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_DESKTOP_PROXY_URL")); raw != "" {
		bind, canonical, err := desktopd.DesktopProxyBind(raw)
		if err != nil {
			// A broken configuration must be fixed before desktops depend on
			// this proxy; fail loudly like the other required env values.
			log.Fatalf("DESKTOPD_DESKTOP_PROXY_URL is invalid: %v", err)
		}
		desktopBind = bind
		desktopCanonical = canonical
	}
	egress := desktopd.NewEgressSource(keyStateDir, desktopd.EgressConfig{
		Upstream:        strings.TrimSpace(os.Getenv("DESKTOPD_UPSTREAM_PROXY")),
		DesktopProxyURL: desktopCanonical,
	})
	svc.Egress = egress
	directSuffixes, err := desktopd.ParseDirectSuffixes(os.Getenv("DESKTOPD_PROXY_DIRECT"))
	if err != nil {
		log.Fatalf("[desktopd] %v", err)
	}
	if len(directSuffixes) > 0 {
		log.Printf("[desktopd] proxy direct suffixes: %s", strings.Join(directSuffixes, ","))
	}
	desktopd.StartProxyErrorSummary(ctx)
	if desktopBind != "" {
		// Per-user egress modes (proxy/direct/smart) live next to the other
		// panel state; the bridge listener maps source IPs to desktops.
		svc.ProxyModes = desktopd.NewProxyModeStore(keyStateDir, svc.DesktopContainerIPs)
		desktopProxy := desktopd.NewForwardProxy(desktopd.ProxyConfig{
			Addr:           desktopBind,
			Token:          token,
			Anonymous:      true,
			Upstreams:      egress,
			DirectSuffixes: directSuffixes,
			Modes:          svc.ProxyModes,
		})
		// A taken port is environmental, not a configuration error: keep the
		// desktop API up and let desktops egress directly, with the failure
		// loud in the log. New desktops simply go unproxied.
		if err := desktopProxy.Start(ctx); err != nil {
			log.Printf("[desktopd] desktop egress proxy disabled: %v", err)
		}
	}
	// DESKTOPD_PROXY=1 runs the outbound forward proxy on its own port so
	// other MaClaw hosts (typically in mainland China) can egress through
	// this one: their dockerd points at http(s)://docker:<key>@<here>:18082.
	// The key defaults to DESKTOPD_TOKEN; DESKTOPD_PROXY_TOKEN overrides.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DESKTOPD_PROXY")), "1") {
		proxyAddr := strings.TrimSpace(os.Getenv("DESKTOPD_PROXY_ADDR"))
		if proxyAddr == "" {
			proxyAddr = desktopd.DefaultForwardProxyAddr
		}
		proxy := desktopd.NewForwardProxy(desktopd.ProxyConfig{
			Addr:           proxyAddr,
			Token:          proxyFallback,
			Keys:           proxyKeys,
			Upstreams:      egress,
			DirectSuffixes: directSuffixes,
			TLSCert:        strings.TrimSpace(os.Getenv("DESKTOPD_PROXY_TLS_CERT")),
			TLSKey:         strings.TrimSpace(os.Getenv("DESKTOPD_PROXY_TLS_KEY")),
		})
		// A taken port or missing token must not take the desktop API down:
		// log loudly and keep serving.
		if err := proxy.Start(ctx); err != nil {
			log.Printf("[desktopd-proxy] forward proxy disabled: %v", err)
		}
	}
	panelDir := stateDir
	if adminDisabled {
		panelDir = ""
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           desktopd.Handler(svc, token, panelDir, proxyKeys, egress),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	displayAddr := addr
	if strings.HasPrefix(displayAddr, ":") {
		displayAddr = "127.0.0.1" + displayAddr
	}
	if len(sources) == 0 {
		log.Printf("[desktopd] image source pulls disabled (DESKTOPD_IMAGE_SOURCE=off)")
	} else {
		// Background only: /v1/health and the admin panel answer while it runs.
		go svc.PrefetchImages(ctx)
	}
	log.Printf("[desktopd] browser and screen ports bind %s", svc.PublishBind)
	log.Printf("desktopd listening on %s", addr)
	if adminDisabled {
		log.Printf("[desktopd] admin panel disabled (DESKTOPD_STATE_DIR=off)")
	} else {
		log.Printf("[desktopd] admin panel at http://%s/admin (state dir: %s)", displayAddr, stateDir)
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		if addrInUse(err) {
			log.Fatalf("Address %s is already in use. Run systemctl stop maclaw-desktopd, or set DESKTOPD_ADDR in .env, then start again.", addr)
		}
		log.Fatal(err)
	}
}

func addrInUse(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "address already in use") || strings.Contains(text, "only one usage of each socket address")
}
