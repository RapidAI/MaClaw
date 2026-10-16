package guiapp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const diagramPNGMaxEncodedBytes = 32 << 20

// SaveDiagramPNG asks where to put a diagram and writes the PNG there.
// The desktop webview drops an <a download> click that happens after the
// canvas has been encoded, so the bitmap is handed to the save dialog instead.
// An empty path means the user cancelled.
func (a *App) SaveDiagramPNG(dataURL string) (string, error) {
	data, err := decodeDiagramPNG(dataURL)
	if err != nil {
		return "", err
	}
	if a == nil || a.ctx == nil {
		return "", fmt.Errorf("无法保存图片")
	}
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存图片 / Save image",
		DefaultFilename: "diagram.png",
		Filters: []runtime.FileFilter{
			{DisplayName: "PNG (*.png)", Pattern: "*.png"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	return writeDiagramPNG(savePath, data)
}

func writeDiagramPNG(savePath string, data []byte) (string, error) {
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		return "", nil
	}
	if info, statErr := os.Stat(savePath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("无法保存图片")
	}
	if filepath.Ext(savePath) == "" {
		savePath += ".png"
	}
	if info, statErr := os.Stat(savePath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("无法保存图片")
	}
	if err := os.WriteFile(savePath, data, 0o644); err != nil {
		return "", err
	}
	return savePath, nil
}

func decodeDiagramPNG(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	lower := strings.ToLower(payload)
	if strings.HasPrefix(lower, "data:") {
		comma := strings.Index(payload, ",")
		if comma < 0 {
			return nil, fmt.Errorf("invalid image")
		}
		header := lower[:comma]
		if !strings.Contains(header, "image/png") || !strings.Contains(header, "base64") {
			return nil, fmt.Errorf("invalid image")
		}
		payload = payload[comma+1:]
	}
	payload = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, payload)
	if payload == "" || len(payload) > diagramPNGMaxEncodedBytes {
		return nil, fmt.Errorf("invalid image")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid image")
	}
	if len(data) < 8 || !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return nil, fmt.Errorf("invalid image")
	}
	return data, nil
}
