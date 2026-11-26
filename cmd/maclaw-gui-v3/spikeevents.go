package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// bindEventSinks wires the CustomEvent verdicts coming back from the spike
// pages into C4/C5/C6/C8 log lines.
func (s *SpikeService) bindEventSinks(app *application.App) {
	app.Event.On("spike:pong", func(e *application.CustomEvent) {
		payload, _ := json.Marshal(e.Data)
		spikeLogf("C8 PASS go→js→go round trip pong payload=%s", payload)
	})
	app.Event.On("spike:probe", func(e *application.CustomEvent) {
		payload, _ := json.Marshal(e.Data)
		spikeLogf("C5 probe sender=%q payload=%s", e.Sender, payload)
	})
	app.Event.On("spike:fetch-result", func(e *application.CustomEvent) {
		payload, _ := json.Marshal(e.Data)
		var ok bool
		if m, isMap := e.Data.(map[string]any); isMap {
			v, _ := m["ok"].(bool)
			st, _ := m["status"].(float64)
			ok = v && st == 200
		}
		if ok {
			spikeLogf("C4 PASS middleware POST /maclaw-spike/append → %s", payload)
		} else {
			spikeLogf("C4 FAIL middleware POST → %s", payload)
		}
	})
	app.Event.On("spike:cookie-set", func(e *application.CustomEvent) {
		payload, _ := json.Marshal(e.Data)
		spikeLogf("C6 second window cookie set: %s", payload)
	})
	app.Event.On("spike:win1-cookie", func(e *application.CustomEvent) {
		payload, _ := json.Marshal(e.Data)
		cookie, _ := e.Data.(string)
		if bytes.Contains([]byte(cookie), []byte("spike2=1")) {
			spikeLogf("C6 RESULT win1 sees win2 cookie → SHARED profile (同一 WebView2 user-data-dir；v3 多窗口可替代 maclawplayer 的 cookie 共享需求)")
		} else {
			spikeLogf("C6 RESULT win1 does NOT see win2 cookie → ISOLATED profile（cookie 隔离；maclawplayer 的 YouTube cookie 需求需另找方案）")
		}
		spikeLogf("C6 win1 cookie=%s", payload)
	})
	app.Event.On("spike:second-ready", func(e *application.CustomEvent) {
		payload, _ := json.Marshal(e.Data)
		spikeLogf("C6 second-ready sender=%q payload=%s", e.Sender, payload)
	})
}

// spikeTrayIconBytes builds a minimal valid 16x16 32bpp ICO (solid teal
// square) — enough for the tray checkpoint, no binary asset needed.
func spikeTrayIconBytes() []byte {
	const size = 16
	var buf bytes.Buffer
	// ICONDIR
	buf.Write([]byte{0, 0, 1, 0, 1, 0})
	// ICONDIRENTRY: 16x16, 32bpp
	imageOffset := uint32(6 + 16)
	bytesInRes := uint32(40 + size*size*4 + (size/8)*size*4) // header + XOR + AND mask rows
	entry := make([]byte, 16)
	entry[0] = size
	entry[1] = size
	binary.LittleEndian.PutUint16(entry[4:], 1)             // planes
	binary.LittleEndian.PutUint16(entry[6:], 32)            // bit count
	binary.LittleEndian.PutUint32(entry[8:], bytesInRes)    // bytes in res
	binary.LittleEndian.PutUint32(entry[12:], imageOffset)  // offset
	buf.Write(entry)
	// BITMAPINFOHEADER (biHeight = 2*size: XOR + AND)
	bih := make([]byte, 40)
	binary.LittleEndian.PutUint32(bih[0:], 40)
	binary.LittleEndian.PutUint32(bih[4:], size)
	binary.LittleEndian.PutUint32(bih[8:], size*2)
	binary.LittleEndian.PutUint16(bih[12:], 1)  // planes
	binary.LittleEndian.PutUint16(bih[14:], 32) // bpp
	buf.Write(bih)
	// XOR: bottom-up rows of BGRA, teal
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			buf.Write([]byte{0x80, 0x00, 0x80, 0xFF}) // B G R A
		}
	}
	// AND mask: fully opaque
	for y := 0; y < size; y++ {
		for i := 0; i < size/8; i++ {
			buf.Write([]byte{0, 0, 0, 0})
		}
	}
	return buf.Bytes()
}
