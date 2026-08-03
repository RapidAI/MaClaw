package main

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
	deviceAmbientRetryInterval   = 5 * time.Minute
	deviceAmbientExpiresAfter    = 2 * time.Hour
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

		delay := deviceAmbientRefreshInterval
		weather, err := a.resolveDeviceAmbientWeather(ctx)
		if err != nil {
			log.Printf("[device-ambient] weather refresh failed: %v", err)
			delay = deviceAmbientRetryInterval
		} else if err := a.pushDeviceAmbientWeather(weather); err != nil {
			log.Printf("[device-ambient] weather push deferred: %v", err)
			delay = deviceAmbientRetryInterval
		} else {
			log.Printf("[device-ambient] weather pushed: %s %dC (%s)", weather.Summary, weather.TemperatureC, weather.Location)
		}
		timer.Reset(delay)
	}
}

func (a *App) pushDeviceAmbientWeather(weather deviceAmbientWeather) error {
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return fmt.Errorf("Hub not connected")
	}
	return hub.SendDeviceGatewayAmbient(weather.Summary, weather.TemperatureC, weather.Location, time.Now().Add(deviceAmbientExpiresAfter))
}

// refreshDeviceAmbientWeatherOnce is also called after every Hub reconnect so
// hardware does not wait for the periodic ticker before receiving fresh state.
func (a *App) refreshDeviceAmbientWeatherOnce() {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	weather, err := a.resolveDeviceAmbientWeather(ctx)
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
	weather, err := a.resolveDeviceAmbientWeather(ctx)
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
	query := fmt.Sprintf("%s 当前天气 实时温度 摄氏度", city)
	searchCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	search, searchErr := websearch.SearchWithStrategyCtx(searchCtx, query, 6, strategy)
	cancel()

	if searchErr == nil && len(search.Results) > 0 {
		weather, parseErr := a.extractDeviceAmbientWeather(ctx, city, search.Results)
		if parseErr == nil {
			return normalizeDeviceAmbientWeather(weather, city)
		}
		searchErr = parseErr
	}

	if searchErr != nil {
		return deviceAmbientWeather{}, fmt.Errorf("structured weather: %v; search fallback: %w", structuredErr, searchErr)
	}
	return deviceAmbientWeather{}, structuredErr
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
	resp, err := doSimpleLLMRequest(llmCtx, llmCfg, messages, &http.Client{Timeout: 30 * time.Second}, 30*time.Second)
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
