package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	deviceAmbientRefreshInterval = 45 * time.Minute
	// A failed resolve may have spent a web search plus an LLM request, so a
	// persistent outage must not turn retries into an upstream metronome.
	deviceAmbientRetryInterval = 15 * time.Minute
	// With no display attached the loop only re-checks for a consumer on
	// this cadence; the check itself is local and costs no network traffic.
	deviceAmbientIdlePoll     = 15 * time.Minute
	deviceAmbientExpiresAfter = 2 * time.Hour
)

type deviceAmbientWeather struct {
	Summary      string `json:"summary"`
	TemperatureC int    `json:"temperatureC"`
	Location     string `json:"location"`
}

func (a *App) startDeviceAmbientWeather(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go a.deviceAmbientWeatherLoop(ctx)
}

func (a *App) deviceAmbientWeatherLoop(ctx context.Context) {
	// Let Hub authentication finish first, without delaying the first useful
	// snapshot after a normal startup.
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if !a.deviceAmbientHasConsumer() {
			// No USB companion board is attached: there is nobody to show
			// the snapshot to, so the resolve must not run at all. Buddy
			// attach replays the cached snapshot or resolves once stale.
			timer.Reset(deviceAmbientIdlePoll)
			continue
		}

		delay := deviceAmbientRefreshInterval
		weather, err := a.deviceAmbientWeatherFresh(ctx)
		if err != nil {
			log.Printf("[device-ambient] weather refresh failed: %v", err)
			delay = deviceAmbientRetryInterval
		} else if err := a.pushDeviceAmbientWeather(weather); err != nil {
			// A push failure is a link problem, not a resolve problem:
			// re-running the pipeline would spend another search + LLM
			// request for nothing. The Hub reconnect hook republishes when
			// the link returns.
			log.Printf("[device-ambient] weather push deferred: %v", err)
		} else {
			log.Printf("[device-ambient] weather pushed: %s %dC (%s)", weather.Summary, weather.TemperatureC, weather.Location)
		}
		timer.Reset(delay)
	}
}

// deviceAmbientHasConsumer reports whether the USB companion board that
// renders the snapshot is attached. With no board there is nobody to show
// the weather to, so nothing may touch the network for it.
func (a *App) deviceAmbientHasConsumer() bool {
	a.buddyMu.Lock()
	defer a.buddyMu.Unlock()
	return a.buddyRunner != nil
}

// ambientWeatherCached returns the last resolved snapshot while it still
// sits inside the device expiry window, so a re-attach inside two hours
// never triggers another weather resolve.
func (a *App) ambientWeatherCached() (deviceAmbientWeather, bool) {
	a.ambientWeatherMu.Lock()
	weather, at := a.ambientWeather, a.ambientWeatherAt
	a.ambientWeatherMu.Unlock()
	if at.IsZero() || time.Since(at) >= deviceAmbientExpiresAfter {
		return deviceAmbientWeather{}, false
	}
	return weather, true
}

func (a *App) storeAmbientWeather(weather deviceAmbientWeather) {
	a.ambientWeatherMu.Lock()
	a.ambientWeather = weather
	a.ambientWeatherAt = time.Now()
	a.ambientWeatherMu.Unlock()
}

// deviceAmbientWeatherFresh returns the cached snapshot while it is inside
// the device expiry window; once stale (or never resolved) it runs the full
// resolve pipeline and caches the result. The double-check inside the
// resolve mutex keeps a concurrent attach from paying for a second resolve.
func (a *App) deviceAmbientWeatherFresh(ctx context.Context) (deviceAmbientWeather, error) {
	if cached, ok := a.ambientWeatherCached(); ok {
		return cached, nil
	}
	a.ambientResolveMu.Lock()
	defer a.ambientResolveMu.Unlock()
	if cached, ok := a.ambientWeatherCached(); ok {
		return cached, nil
	}
	return a.resolveAndCacheDeviceAmbientWeatherLocked(ctx)
}

// resolveAndCacheDeviceAmbientWeather serializes every resolve — automatic
// and manual — so a settings refresh landing on a loop tick cannot pay for
// two weather pipelines at once.
func (a *App) resolveAndCacheDeviceAmbientWeather(ctx context.Context) (deviceAmbientWeather, error) {
	a.ambientResolveMu.Lock()
	defer a.ambientResolveMu.Unlock()
	return a.resolveAndCacheDeviceAmbientWeatherLocked(ctx)
}

// resolveAndCacheDeviceAmbientWeatherLocked is the resolve+cache body; the
// caller must hold ambientResolveMu.
func (a *App) resolveAndCacheDeviceAmbientWeatherLocked(ctx context.Context) (deviceAmbientWeather, error) {
	weather, err := a.resolveDeviceAmbientWeather(ctx)
	if err == nil {
		a.storeAmbientWeather(weather)
	}
	return weather, err
}

func (a *App) pushDeviceAmbientWeather(weather deviceAmbientWeather) error {
	// The USB companion shares the same resolved weather and must not depend on
	// Hub connectivity, so forward before the Hub gate.
	a.setBuddyWeather(weather.Summary, weather.Location, weather.TemperatureC)
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return fmt.Errorf("Hub not connected")
	}
	return hub.SendDeviceGatewayAmbient(weather.Summary, weather.TemperatureC, weather.Location, time.Now().Add(deviceAmbientExpiresAfter))
}

// refreshDeviceAmbientWeatherOnce is also called after every Hub reconnect so
// hardware does not wait for the periodic ticker before receiving fresh state.
// Without an attached board the reconnect alone is no reason to resolve, and
// a still-fresh snapshot is repushed without hitting the network.
func (a *App) refreshDeviceAmbientWeatherOnce() {
	if !a.deviceAmbientHasConsumer() {
		return
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Bound the wait on ambientResolveMu as well as the resolve itself: an
	// unbounded call could park behind a slow resolve and stall the loop.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	weather, err := a.deviceAmbientWeatherFresh(ctx)
	if err != nil {
		log.Printf("[device-ambient] reconnect refresh failed: %v", err)
		return
	}
	if err := a.pushDeviceAmbientWeather(weather); err != nil {
		log.Printf("[device-ambient] reconnect push failed: %v", err)
	}
}

// RefreshDeviceAmbientWeather is exposed to the settings panel so changing
// the city takes effect immediately instead of waiting for the 45-minute loop.
// It returns the resolved location so an automatic network lookup can be
// displayed in the settings UI without turning it into a manual city override.
func (a *App) RefreshDeviceAmbientWeather() (string, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Interactive entry point: keep the whole resolve bounded so the settings
	// panel cannot spin for the full multi-engine strategy budget.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	weather, err := a.resolveAndCacheDeviceAmbientWeather(ctx)
	if err != nil {
		return "", err
	}
	if err := a.pushDeviceAmbientWeather(weather); err != nil {
		return "", err
	}
	return strings.TrimSpace(weather.Location), nil
}

func (a *App) resolveDeviceAmbientWeather(ctx context.Context) (deviceAmbientWeather, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return deviceAmbientWeather{}, err
	}
	city := strings.TrimSpace(cfg.PetAmbientCity)
	if city == "" {
		city = "当前位置"
	}
	// Prefer the structured, no-key provider. It returns a dedicated current
	// temperature field, avoids mistaking a forecast high/low for the current
	// reading, and does not spend an LLM request on every periodic refresh.
	// Web search + MaClaw extraction remains the fallback for locations the
	// geocoder cannot resolve or networks that block Open-Meteo.
	weather, structuredErr := fetchOpenMeteoWeather(ctx, city)
	if structuredErr == nil {
		return normalizeDeviceAmbientWeather(weather, city)
	}
	strategy := websearch.MigrateLegacyWebSearchStrategy(cfg.WebSearchStrategy, cfg.WebSearchProviders, cfg.WebSearchCurrentProvider)
	// The ambient refresh runs unattended on a 45-minute loop: it must go
	// through the configured web_search channels in order but never launch
	// the managed browser on the user's desktop.
	strategy = webSearchStrategyNoBrowser(strategy)
	query := fmt.Sprintf("%s 当前天气 实时温度 摄氏度", city)
	// SearchWithStrategyCtx enforces the per-strategy engine budget itself;
	// this outer timeout is only a safety net above its largest budget — the
	// authenticated MaClaw Hub channel is allowed up to
	// WebSearchMaclawHubTimeout, and a tighter cap here would truncate it.
	searchCtx, cancel := context.WithTimeout(ctx, websearch.WebSearchMaclawHubTimeout+30*time.Second)
	search, searchErr := websearch.SearchWithStrategyCtx(searchCtx, query, 6, strategy)
	cancel()

	if searchErr == nil && len(search.Results) > 0 {
		weather, parseErr := a.extractDeviceAmbientWeather(ctx, city, search.Results)
		if parseErr == nil {
			return normalizeDeviceAmbientWeather(weather, city)
		}
		searchErr = parseErr
	}
	if searchErr == nil {
		searchErr = fmt.Errorf("no results")
	}
	return deviceAmbientWeather{}, fmt.Errorf("structured weather: %v; search fallback: %w", structuredErr, searchErr)
}

func (a *App) extractDeviceAmbientWeather(ctx context.Context, city string, results []websearch.SearchResult) (deviceAmbientWeather, error) {
	llmCfg := a.GetMaclawLLMConfig()
	if strings.TrimSpace(llmCfg.URL) == "" || strings.TrimSpace(llmCfg.Model) == "" {
		return deviceAmbientWeather{}, fmt.Errorf("MaClaw LLM is not configured")
	}
	var evidence strings.Builder
	for i, item := range results {
		fmt.Fprintf(&evidence, "%d. %s\n%s\n%s\n", i+1, item.Title, item.URL, item.Snippet)
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": `Extract current weather from search snippets. Return JSON only: {"summary":"short Chinese condition","temperatureC":integer,"location":"short city"}. Do not use forecast highs/lows or invent missing values.`},
		map[string]string{"role": "user", "content": "Requested location: " + city + "\nSearch evidence:\n" + evidence.String()},
	}
	llmCtx := llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "device-ambient-weather", OwnerID: "device-ambient"})
	// No client-level Timeout: the helper enforces the budget per request and
	// a client Timeout would kill the connection at ~30s, truncating the P0-3
	// detached read before its grace window (adoption would always fail).
	resp, err := doSimpleLLMRequest(llmCtx, llmCfg, messages, nil, 30*time.Second)
	if err != nil {
		return deviceAmbientWeather{}, err
	}
	return parseDeviceAmbientWeatherJSON(resp.Content)
}

func parseDeviceAmbientWeatherJSON(raw string) (deviceAmbientWeather, error) {
	raw = strings.TrimSpace(raw)
	if start := strings.IndexByte(raw, '{'); start >= 0 {
		if end := strings.LastIndexByte(raw, '}'); end > start {
			raw = raw[start : end+1]
		}
	}
	var weather deviceAmbientWeather
	if err := json.Unmarshal([]byte(raw), &weather); err != nil {
		return weather, fmt.Errorf("invalid weather JSON: %w", err)
	}
	return weather, nil
}

func normalizeDeviceAmbientWeather(weather deviceAmbientWeather, fallbackLocation string) (deviceAmbientWeather, error) {
	weather.Summary = strings.TrimSpace(weather.Summary)
	weather.Location = strings.TrimSpace(weather.Location)
	if weather.Location == "" || weather.Location == "当前位置" {
		weather.Location = strings.TrimSpace(fallbackLocation)
	}
	if weather.Summary == "" {
		return weather, fmt.Errorf("weather summary is empty")
	}
	if weather.TemperatureC < -80 || weather.TemperatureC > 80 {
		return weather, fmt.Errorf("weather temperature is out of range")
	}
	// Keep within the hardware's current compact weather line and known glyphs.
	for _, item := range []struct{ from, to string }{
		{"partly cloudy", "多云"}, {"cloudy", "多云"}, {"overcast", "阴"},
		{"clear", "晴"}, {"sunny", "晴"}, {"thunderstorm", "雷雨"},
		{"drizzle", "小雨"}, {"rain", "雨"}, {"snow", "雪"},
		{"fog", "雾"}, {"haze", "霾"},
	} {
		if strings.EqualFold(weather.Summary, item.from) {
			weather.Summary = item.to
			break
		}
	}
	if len([]rune(weather.Summary)) > 4 {
		weather.Summary = string([]rune(weather.Summary)[:4])
	}
	return weather, nil
}

func fetchOpenMeteoWeather(ctx context.Context, city string) (deviceAmbientWeather, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	latitude, longitude := 0.0, 0.0
	location := strings.TrimSpace(city)
	if location == "" || location == "当前位置" {
		var ipGeo struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			City      string  `json:"city"`
		}
		if err := getDeviceAmbientJSON(ctx, client, "https://ipapi.co/json/", &ipGeo); err != nil {
			return deviceAmbientWeather{}, fmt.Errorf("locate desktop: %w", err)
		}
		latitude, longitude, location = ipGeo.Latitude, ipGeo.Longitude, ipGeo.City
	} else {
		var geo struct {
			Results []struct {
				Name      string  `json:"name"`
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"results"`
		}
		endpoint := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=zh&name=" + url.QueryEscape(location)
		if err := getDeviceAmbientJSON(ctx, client, endpoint, &geo); err != nil || len(geo.Results) == 0 {
			if err == nil {
				err = fmt.Errorf("city not found")
			}
			return deviceAmbientWeather{}, fmt.Errorf("geocode %s: %w", location, err)
		}
		latitude, longitude = geo.Results[0].Latitude, geo.Results[0].Longitude
		if geo.Results[0].Name != "" {
			location = geo.Results[0].Name
		}
	}
	var current struct {
		Current struct {
			Temperature float64 `json:"temperature_2m"`
			WeatherCode int     `json:"weather_code"`
		} `json:"current"`
	}
	endpoint := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,weather_code&timezone=auto", latitude, longitude)
	if err := getDeviceAmbientJSON(ctx, client, endpoint, &current); err != nil {
		return deviceAmbientWeather{}, err
	}
	return deviceAmbientWeather{Summary: openMeteoWeatherSummary(current.Current.WeatherCode), TemperatureC: int(math.Round(current.Current.Temperature)), Location: location}, nil
}

func getDeviceAmbientJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MaClaw-GUI/ambient-weather")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
}

func openMeteoWeatherSummary(code int) string {
	switch {
	case code == 0:
		return "晴"
	case code <= 3:
		return "多云"
	case code == 45 || code == 48:
		return "雾"
	case code >= 51 && code <= 57:
		return "小雨"
	case code >= 61 && code <= 67:
		return "雨"
	case code >= 71 && code <= 77:
		return "雪"
	case code >= 80 && code <= 82:
		return "阵雨"
	case code >= 85 && code <= 86:
		return "阵雪"
	case code >= 95:
		return "雷雨"
	default:
		return "多云"
	}
}
