// Command buddy-host is the development companion for the buddy firmware.
//
// It pushes mock task/weather data over the USB serial link so the device UI
// can be exercised without the MaClaw GUI. Production runs inside guiapp.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
	"github.com/RapidAI/CodeClaw/buddy/serialport"
	"github.com/RapidAI/CodeClaw/buddy/service"
)

func main() {
	list := flag.Bool("list", false, "list serial ports and exit")
	portName := flag.String("port", "", "serial port (default: first Espressif/CP210x/CH34x)")
	idle := flag.Bool("idle", false, "push zero tasks (standby pet page)")
	titlesFile := flag.String("titles", "", "JSON array of titles to push one at a time (font coverage check)")
	celebrateDemo := flag.Bool("celebrate-demo", false, "push a fake task completion and watch the celebration page")
	petDemo := flag.Bool("pet-demo", false, "send a synthetic 128x128 pet asset and weather, then exit")
	verbose := flag.Bool("v", true, "log link events")
	duration := flag.Duration("duration", 0, "exit after this long (e.g. 8s); 0 = run until ctrl-c")
	flag.Parse()

	if *list {
		infos, err := serialport.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, "list ports:", err)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(infos)
		return
	}

	opts := service.DefaultOptions()
	if *verbose {
		opts.Logf = func(format string, args ...any) {
			fmt.Printf("%s [buddy] %s\n", time.Now().Format("15:04:05.000"),
				fmt.Sprintf(format, args...))
		}
	}

	var runner *service.Runner
	dial := func() (io.ReadWriteCloser, error) {
		port, name, err := serialport.Dialer(*portName)
		if err != nil {
			return nil, err
		}
		if runner != nil {
			runner.SetPort(name())
		}
		return port, nil
	}
	runner = service.NewRunner(dial, opts)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	go runner.Run(ctx)

	if *titlesFile != "" {
		runFontCheck(ctx, runner, *titlesFile)
		return
	}
	if *celebrateDemo {
		runCelebrateDemo(ctx, runner)
		return
	}
	if *petDemo {
		runPetDemo(ctx, runner)
		return
	}

	mock := mockSource{}
	runner.SetWeather(mock.weather())
	runner.SetSnapshot(mock.snapshot(*idle))

	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	weatherTicker := time.NewTicker(30 * time.Minute)
	defer weatherTicker.Stop()
	statusTicker := time.NewTicker(2 * time.Second)
	defer statusTicker.Stop()

	fmt.Printf("buddy-host: pushing %s; ctrl-c to stop\n", portLabel(*portName))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runner.SetSnapshot(mock.snapshot(*idle))
		case <-weatherTicker.C:
			mock.weatherTemp++
			runner.SetWeather(mock.weather())
		case <-statusTicker.C:
			s := runner.Status()
			fmt.Printf("status: connected=%v port=%s fw=%s board=%s touch=%v sound=%v page=%s ack=%s err=%s\n",
				s.Connected, s.Port, s.DeviceFW, s.DeviceBoard, s.DeviceTouch, s.DeviceSound, s.Page,
				s.LastAckAt.Format("15:04:05.000"), s.LastError)
		}
	}
}

// runFontCheck pushes one real task title at a time and reports the device's
// cumulative missing-glyph counter (reported in its heartbeat). The device UI
// renders the single current task immediately, so one second per title is
// enough for every title to be drawn.
func runFontCheck(ctx context.Context, runner *service.Runner, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read titles:", err)
		return
	}
	var titles []string
	if err := json.Unmarshal(raw, &titles); err != nil {
		fmt.Fprintln(os.Stderr, "parse titles:", err)
		return
	}
	if len(titles) == 0 {
		fmt.Fprintln(os.Stderr, "titles file is empty")
		return
	}
	// Wait for the link and the first heartbeat.
	deadline := time.Now().Add(10 * time.Second)
	for !runner.Status().Connected && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	for i, title := range titles {
		runner.SetSnapshot(protocol.Snapshot{
			Counts: protocol.Counts{
				Total:      len(titles),
				InProgress: 1,
				Done:       len(titles) - 1,
			},
			Current: []protocol.TaskItem{{
				ID:       fmt.Sprintf("title-%d", i),
				Title:    title,
				Progress: -1,
			}},
			UpdatedAt: time.Now().UnixMilli(),
		})
		fmt.Printf("pushed %d/%d: %s\n", i+1, len(titles), title)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}

	// Let at least two heartbeats carry the final counter.
	select {
	case <-ctx.Done():
		return
	case <-time.After(7 * time.Second):
	}
	s := runner.Status()
	fmt.Printf("FONT_CHECK titles=%d device_missing_glyphs=%d connected=%v fw=%s page=%s\n",
		len(titles), s.DeviceMissingGlyphs, s.Connected, s.DeviceFW, s.Page)
	if !s.Connected {
		// A disconnected run must never read as a pass: the counter was never
		// exercised on the device.
		fmt.Fprintln(os.Stderr, "FONT_CHECK FAILED: device not connected")
		os.Exit(1)
	}
}

// runCelebrateDemo pushes one in-progress task and then completes it, which
// makes the service emit the celebrate frame. The device reports "celebrate"
// in its heartbeat while the page is active, then returns to standby.
func runCelebrateDemo(ctx context.Context, runner *service.Runner) {
	waitUntil := time.Now().Add(10 * time.Second)
	for !runner.Status().Connected && time.Now().Before(waitUntil) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	runner.SetSnapshot(protocol.Snapshot{
		Counts: protocol.Counts{Total: 2, InProgress: 1, Done: 1},
		Current: []protocol.TaskItem{
			{ID: "demo-1", Title: "庆祝屏验证任务", Progress: -1},
		},
		UpdatedAt: time.Now().UnixMilli(),
	})
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
	}
	runner.SetSnapshot(protocol.Snapshot{
		Counts:    protocol.Counts{Total: 2, InProgress: 0, Done: 2},
		UpdatedAt: time.Now().UnixMilli(),
	})

	seen := false
	for i := 0; i < 20; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		s := runner.Status()
		if s.Page == "celebrate" {
			seen = true
		}
		fmt.Printf("celebrate-demo page=%s\n", s.Page)
	}
	fmt.Printf("CELEBRATE_DEMO seen=%v connected=%v\n", seen, runner.Status().Connected)
}

// runPetDemo pushes a synthetic animated pet asset so the asset transfer and
// the ring standby layout can be checked without the GUI.
func runPetDemo(ctx context.Context, runner *service.Runner) {
	waitUntil := time.Now().Add(10 * time.Second)
	for !runner.Status().Connected && time.Now().Before(waitUntil) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	runner.SetWeather(protocol.Weather{Summary: "晴", TemperatureC: 23, Location: "北京"})
	frames := make([][]byte, 0, protocol.AssetMaxFrames)
	for i := 0; i < protocol.AssetMaxFrames; i++ {
		frames = append(frames, demoPetFrame(i))
	}
	runner.SetPetAsset(frames, 450)
	for i := 0; i < 16; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		s := runner.Status()
		fmt.Printf("pet-demo page=%s fw=%s pet_ready=%v\n", s.Page, s.DeviceFW, s.PetReady)
	}
	fmt.Printf("PET_DEMO sent=%d frames ready=%v connected=%v\n",
		protocol.AssetMaxFrames, runner.Status().PetReady, runner.Status().Connected)
}

// demoPetFrame draws a bobbing synthetic mascot; the frame index drives the
// vertical offset so an 8-frame transfer is visibly animated.
func demoPetFrame(index int) []byte {
	const size = protocol.AssetWidth
	bob := [...]int{0, -3, -4, -3, 0, 3, 4, 3}[index%8]
	frame := make([]byte, size*size*3)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			yy := y - bob
			dx, dy := float64(x-64), float64(yy-64)
			inside := dx*dx+dy*dy <= 42*42
			earL := (x-38)*(x-38)+(yy-26)*(yy-26) <= 14*14
			earR := (x-90)*(x-90)+(yy-26)*(yy-26) <= 14*14
			eyeL := (x-52)*(x-52)+(yy-58)*(yy-58) <= 6*6
			eyeR := (x-76)*(x-76)+(yy-58)*(yy-58) <= 6*6
			if !inside && !earL && !earR {
				continue
			}
			r, g, b := byte(0x60), byte(0xA5), byte(0xFA)
			if eyeL || eyeR {
				r, g, b = 0x0E, 0x11, 0x16
			}
			value := uint16((uint16(r)&0xF8)<<8 | (uint16(g)&0xFC)<<3 | uint16(b)>>3)
			i := (y*size + x) * 3
			frame[i], frame[i+1], frame[i+2] = byte(value), byte(value>>8), 255
		}
	}
	return frame
}

func portLabel(name string) string {
	if name == "" {
		return "the first detected buddy port"
	}
	return name
}

type mockSource struct {
	tick        int
	weatherTemp int
}

func (m *mockSource) snapshot(idle bool) protocol.Snapshot {
	m.tick++
	now := time.Now().UnixMilli()
	if idle {
		return protocol.Snapshot{
			Counts:    protocol.Counts{Total: 6, Done: 6},
			UpdatedAt: now,
		}
	}
	// Cycle through busy/idle so the device page switch is exercised.
	inProgress := m.tick % 4
	current := []protocol.TaskItem{}
	titles := []string{
		"整理会议纪要并导出 Word（终稿）",
		"文档导入：季度报表.xlsx",
		"检查 SSH 部署脚本",
	}
	for i := 0; i < inProgress && i < len(titles); i++ {
		current = append(current, protocol.TaskItem{
			ID:       fmt.Sprintf("mock-%d", i),
			Title:    titles[i],
			Progress: float64((m.tick*7)%100) / 100.0,
		})
	}
	done := 4
	if inProgress > 0 {
		done = 3
	}
	return protocol.Snapshot{
		Counts: protocol.Counts{
			Total:      done + inProgress + 1,
			InProgress: inProgress,
			Pending:    1,
			Done:       done,
		},
		Current:   current,
		UpdatedAt: now,
	}
}

func (m *mockSource) weather() protocol.Weather {
	if m.weatherTemp == 0 {
		m.weatherTemp = 23
	}
	return protocol.Weather{Summary: "晴", TemperatureC: m.weatherTemp, Location: "北京"}
}
