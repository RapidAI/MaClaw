package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/google/uuid"
)

// Max recorded upload size (~3h mono 16kHz 16-bit WAV plus header slack).
// Prevents runaway base64 payloads from OOMing the process.
const maxRecordedAudioBytes = 400 * 1024 * 1024

// Max single chunk for streaming upload (~512 KiB binary → ~700 KiB base64).
const maxRecordedAudioChunkBytes = 512 * 1024

// Abandon incomplete streaming uploads older than this (abandoned tab / crash).
const recordedUploadSessionTTL = 4 * time.Hour

// recordedUploadSession streams a WAV from the frontend so the process never
// holds a multi-hour base64 string + full decoded buffer.
//
// Two modes:
//   - classic: client appends a complete WAV (header+PCM) in chunks
//   - live:    server writes a placeholder header at Begin; client streams PCM
//     only during capture; Finish patches the real RIFF sizes
type recordedUploadSession struct {
	mu         sync.Mutex
	id         string
	title      string
	path       string // final destination path
	partPath   string // temp .part while appending
	file       *os.File
	written    int64
	createdAt  time.Time
	closed     bool
	live       bool // true → PCM-only appends; header patched on Finish
	sampleRate int  // live mode sample rate (default 16000)
}

var (
	recordDumpCounter  int64
	recordSaveCounter  int64
	recordedUploadMu   sync.Mutex
	recordedUploadByID = map[string]*recordedUploadSession{}
)

// SaveRecordedAudioBase64 saves a full base64 WAV in one shot (small clips /
// backward compatible). Long recordings should use Begin/Append/Finish instead.
func (a *App) SaveRecordedAudioBase64(wavBase64 string, title string) (map[string]interface{}, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	title = strings.TrimSpace(title)
	wavBase64 = strings.TrimSpace(wavBase64)
	detail := recordDetailEnabled()
	if detail {
		log.Printf("[record-audio] save begin title=%q b64_len=%d mode=oneshot", title, len(wavBase64))
	}
	if wavBase64 == "" {
		return nil, fmt.Errorf("empty audio data")
	}
	if len(wavBase64) > maxRecordedAudioBytes*4/3+1024 {
		return nil, fmt.Errorf("audio data too large")
	}
	wavData, err := base64.StdEncoding.DecodeString(wavBase64)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	return a.finalizeRecordedWAV(wavData, title, time.Now())
}

// BeginRecordedAudioUpload starts a classic streaming upload (client sends full WAV).
func (a *App) BeginRecordedAudioUpload(title string) (map[string]interface{}, error) {
	return a.beginRecordedAudioUpload(title, false, 16000)
}

// BeginLiveRecordedAudioUpload starts a live capture session: server writes a
// placeholder WAV header immediately; client streams PCM only during recording
// (binary HTTP or AppendRecordedAudioBytes). Finish patches RIFF sizes.
func (a *App) BeginLiveRecordedAudioUpload(title string) (map[string]interface{}, error) {
	return a.beginRecordedAudioUpload(title, true, 16000)
}

func (a *App) beginRecordedAudioUpload(title string, live bool, sampleRate int) (map[string]interface{}, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	title = strings.TrimSpace(title)
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	dir := filepath.Join(a.GetDataDir(), "recordings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create recordings dir: %w", err)
	}
	safeTitle := sanitizeRecordingFileTitle(title)
	now := time.Now()
	stamp := now.Format("20060102-150405") + fmt.Sprintf("-%03d", now.Nanosecond()/1e6)
	seq := atomic.AddInt64(&recordSaveCounter, 1)
	name := fmt.Sprintf("%s-%s-%03d.wav", safeTitle, stamp, seq%1000)
	finalPath := filepath.Join(dir, name)
	partPath := finalPath + ".part"

	// RDWR so live sessions can Seek(0) and patch the WAV header on Finish.
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create upload part: %w", err)
	}
	id := uuid.NewString()
	sess := &recordedUploadSession{
		id:         id,
		title:      title,
		path:       finalPath,
		partPath:   partPath,
		file:       f,
		createdAt:  now,
		live:       live,
		sampleRate: sampleRate,
	}
	if live {
		// Placeholder header (data size 0); rewritten on Finish with real PCM length.
		hdr := makeWAVHeaderBytes(0, sampleRate)
		if _, err := f.Write(hdr); err != nil {
			_ = f.Close()
			_ = os.Remove(partPath)
			return nil, fmt.Errorf("write placeholder wav header: %w", err)
		}
		sess.written = int64(len(hdr))
	}
	recordedUploadMu.Lock()
	pruneStaleRecordedUploadsLocked(time.Now())
	// Bound concurrent upload sessions per process.
	if len(recordedUploadByID) >= 8 {
		recordedUploadMu.Unlock()
		_ = f.Close()
		_ = os.Remove(partPath)
		return nil, fmt.Errorf("too many concurrent recording uploads")
	}
	recordedUploadByID[id] = sess
	recordedUploadMu.Unlock()

	if recordDetailEnabled() {
		log.Printf("[record-audio] upload begin session=%s live=%v title=%q part=%s", id, live, title, partPath)
	}
	out := map[string]interface{}{
		"session_id":  id,
		"title":       title,
		"live":        live,
		"sample_rate": sampleRate,
		// FE uses this path for true binary POST (no base64).
		"append_path": "/maclaw-record/v1/append",
	}
	return out, nil
}

// AppendRecordedAudioBase64 appends one base64-encoded binary chunk to an upload.
func (a *App) AppendRecordedAudioBase64(sessionID, chunkBase64 string) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	chunkBase64 = strings.TrimSpace(chunkBase64)
	if sessionID == "" || chunkBase64 == "" {
		return fmt.Errorf("session_id and chunk required")
	}
	// Base64 expands ~4/3; reject oversized payloads before decode to bound peak memory.
	maxB64 := maxRecordedAudioChunkBytes*4/3 + 1024
	if len(chunkBase64) > maxB64 {
		return fmt.Errorf("chunk too large: base64 len %d (max %d ≈ %d binary bytes)",
			len(chunkBase64), maxB64, maxRecordedAudioChunkBytes)
	}
	raw, err := base64.StdEncoding.DecodeString(chunkBase64)
	if err != nil {
		return fmt.Errorf("decode chunk: %w", err)
	}
	return a.appendRecordedAudioRaw(sessionID, raw)
}

// AppendRecordedAudioBytes appends raw bytes (Wails maps []byte ↔ base64 string).
// Prefer the binary HTTP append endpoint for large live PCM to avoid base64 overhead.
func (a *App) AppendRecordedAudioBytes(sessionID string, data []byte) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	return a.appendRecordedAudioRaw(strings.TrimSpace(sessionID), data)
}

func (a *App) appendRecordedAudioRaw(sessionID string, raw []byte) error {
	if sessionID == "" {
		return fmt.Errorf("session_id required")
	}
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxRecordedAudioChunkBytes {
		return fmt.Errorf("chunk too large: %d bytes (max %d)", len(raw), maxRecordedAudioChunkBytes)
	}

	sess := lookupRecordedUpload(sessionID)
	if sess == nil {
		return fmt.Errorf("upload session not found")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed || sess.file == nil {
		return fmt.Errorf("upload session closed")
	}
	// Live mode streams PCM16 only — odd lengths would desync the sample stream.
	if sess.live && len(raw)%2 != 0 {
		return fmt.Errorf("live pcm chunk length must be even (got %d)", len(raw))
	}
	if sess.written+int64(len(raw)) > maxRecordedAudioBytes {
		return fmt.Errorf("audio data too large (max %d bytes)", maxRecordedAudioBytes)
	}
	// Write full chunk (os.File.Write may be short without error on some platforms).
	for off := 0; off < len(raw); {
		n, err := sess.file.Write(raw[off:])
		if n > 0 {
			off += n
			sess.written += int64(n)
		}
		if err != nil {
			return fmt.Errorf("write chunk: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("write chunk: short write with no progress")
		}
	}
	return nil
}

// FinishRecordedAudioUpload finalizes a streaming upload into a product WAV.
// Does not load the full multi-hour file into memory — validates via header + size
// and optionally samples PCM for detail stats.
func (a *App) FinishRecordedAudioUpload(sessionID string) (map[string]interface{}, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	sess := takeRecordedUpload(sessionID)
	if sess == nil {
		return nil, fmt.Errorf("upload session not found")
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return nil, fmt.Errorf("upload session closed")
	}
	sess.closed = true
	if sess.file != nil {
		// Live sessions streamed PCM only — patch RIFF header with final sizes.
		if sess.live {
			if sess.written < 44 {
				_ = sess.file.Close()
				sess.file = nil
				_ = os.Remove(sess.partPath)
				return nil, fmt.Errorf("audio data too short")
			}
			dataBytes := sess.written - 44
			sr := sess.sampleRate
			if sr <= 0 {
				sr = 16000
			}
			if _, err := sess.file.Seek(0, io.SeekStart); err != nil {
				_ = sess.file.Close()
				sess.file = nil
				_ = os.Remove(sess.partPath)
				return nil, fmt.Errorf("seek wav header: %w", err)
			}
			hdr := makeWAVHeaderBytes(dataBytes, sr)
			if _, err := sess.file.Write(hdr); err != nil {
				_ = sess.file.Close()
				sess.file = nil
				_ = os.Remove(sess.partPath)
				return nil, fmt.Errorf("patch wav header: %w", err)
			}
			// written already counts header+pcm; sizes only changed in-place.
		}
		// Flush to stable storage before rename so a crash mid-finalize
		// cannot leave a truncated product file that looks complete.
		if err := sess.file.Sync(); err != nil {
			_ = sess.file.Close()
			sess.file = nil
			_ = os.Remove(sess.partPath)
			return nil, fmt.Errorf("sync upload part: %w", err)
		}
		if err := sess.file.Close(); err != nil {
			sess.file = nil
			_ = os.Remove(sess.partPath)
			return nil, fmt.Errorf("close upload part: %w", err)
		}
		sess.file = nil
	}
	if sess.written < 44 {
		_ = os.Remove(sess.partPath)
		return nil, fmt.Errorf("audio data too short")
	}
	if fi, err := os.Stat(sess.partPath); err != nil {
		_ = os.Remove(sess.partPath)
		return nil, fmt.Errorf("stat upload part: %w", err)
	} else if fi.Size() != sess.written {
		_ = os.Remove(sess.partPath)
		return nil, fmt.Errorf("upload size mismatch: disk %d tracked %d", fi.Size(), sess.written)
	}

	// Move to final path first (rename is O(1) on same volume).
	if err := os.Rename(sess.partPath, sess.path); err != nil {
		if err2 := copyFileReplace(sess.partPath, sess.path); err2 != nil {
			_ = os.Remove(sess.partPath)
			return nil, fmt.Errorf("finalize path: %w", err2)
		}
		_ = os.Remove(sess.partPath)
	}

	info, err := a.finalizeRecordedWAVFile(sess.path, sess.title, sess.createdAt)
	if err != nil {
		_ = os.Remove(sess.path)
		return nil, err
	}
	if recordDetailEnabled() {
		log.Printf("[record-audio] upload finish session=%s path=%s size=%v", sessionID, sess.path, info["size_bytes"])
	}
	return info, nil
}

// CancelRecordedAudioUpload aborts a streaming upload and deletes the temp part.
func (a *App) CancelRecordedAudioUpload(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	sess := takeRecordedUpload(sessionID)
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.closed = true
	if sess.file != nil {
		_ = sess.file.Close()
		sess.file = nil
	}
	_ = os.Remove(sess.partPath)
	if recordDetailEnabled() {
		log.Printf("[record-audio] upload cancel session=%s", sessionID)
	}
	return nil
}

func lookupRecordedUpload(id string) *recordedUploadSession {
	recordedUploadMu.Lock()
	defer recordedUploadMu.Unlock()
	return recordedUploadByID[id]
}

func takeRecordedUpload(id string) *recordedUploadSession {
	recordedUploadMu.Lock()
	defer recordedUploadMu.Unlock()
	sess := recordedUploadByID[id]
	delete(recordedUploadByID, id)
	return sess
}

// pruneStaleRecordedUploadsLocked drops abandoned upload sessions.
// Caller must hold recordedUploadMu.
func pruneStaleRecordedUploadsLocked(now time.Time) {
	for id, sess := range recordedUploadByID {
		if sess == nil {
			delete(recordedUploadByID, id)
			continue
		}
		if now.Sub(sess.createdAt) < recordedUploadSessionTTL {
			continue
		}
		delete(recordedUploadByID, id)
		go discardRecordedUploadSession(sess)
	}
}

func discardRecordedUploadSession(sess *recordedUploadSession) {
	if sess == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return
	}
	sess.closed = true
	if sess.file != nil {
		_ = sess.file.Close()
		sess.file = nil
	}
	if sess.partPath != "" {
		_ = os.Remove(sess.partPath)
	}
	if recordDetailEnabled() {
		log.Printf("[record-audio] pruned stale upload session=%s age=%s", sess.id, time.Since(sess.createdAt).Round(time.Second))
	}
}

func copyFileReplace(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// finalizeRecordedWAV validates and persists a full in-memory WAV (one-shot path).
func (a *App) finalizeRecordedWAV(wavData []byte, title string, started time.Time) (map[string]interface{}, error) {
	if len(wavData) < 44 {
		return nil, fmt.Errorf("audio data too short")
	}
	if len(wavData) > maxRecordedAudioBytes {
		return nil, fmt.Errorf("audio data too large (max %d bytes)", maxRecordedAudioBytes)
	}
	dir := filepath.Join(a.GetDataDir(), "recordings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create recordings dir: %w", err)
	}
	safeTitle := sanitizeRecordingFileTitle(title)
	now := time.Now()
	stamp := now.Format("20060102-150405") + fmt.Sprintf("-%03d", now.Nanosecond()/1e6)
	seq := atomic.AddInt64(&recordSaveCounter, 1)
	name := fmt.Sprintf("%s-%s-%03d.wav", safeTitle, stamp, seq%1000)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, wavData, 0o644); err != nil {
		return nil, fmt.Errorf("write audio: %w", err)
	}
	// Prefer path-based finalize so detail stats use the same code path as uploads.
	return a.finalizeRecordedWAVFile(path, title, started)
}

// finalizeRecordedWAVFile validates a product WAV already on disk without loading
// the entire multi-hour body into memory.
func (a *App) finalizeRecordedWAVFile(path, title string, started time.Time) (map[string]interface{}, error) {
	start := time.Now()
	if started.IsZero() {
		started = start
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat audio: %w", err)
	}
	size := fi.Size()
	if size < 44 {
		return nil, fmt.Errorf("audio data too short")
	}
	if size > maxRecordedAudioBytes {
		return nil, fmt.Errorf("audio data too large (max %d bytes)", maxRecordedAudioBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	headerBuf := make([]byte, 44)
	if _, err := io.ReadFull(f, headerBuf); err != nil {
		return nil, fmt.Errorf("read wav header: %w", err)
	}
	header := inspectRecordWAVHeader(headerBuf)
	if err := validateRecordedWAV(header, int(size)); err != nil {
		log.Printf("[record-audio] save failed: invalid wav title=%q err=%v header=%+v size=%d", title, err, header, size)
		return nil, err
	}

	// Duration from on-disk size (more reliable than header dataBytes alone).
	durationSec := wavDurationFromSize(size, header)
	sampleRate := header.SampleRate
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	name := filepath.Base(abs)
	safeTitle := sanitizeRecordingFileTitle(title)
	stamp := started.Format("20060102-150405")

	out := map[string]interface{}{
		"path":         abs,
		"file_name":    name,
		"size_bytes":   size,
		"duration_sec": durationSec,
		"format":       "wav",
		"sample_rate":  sampleRate,
	}

	// Best-effort MP3 archive product (sibling of WAV). Failure never fails the save:
	// original WAV remains the source of truth; agent can still convert via ffmpeg.
	mp3Path, mp3Size, mp3Err := archiveRecordedWAVAsMP3(abs, durationSec)
	if mp3Err != nil {
		log.Printf("[record-audio] mp3 archive skipped path=%s duration=%.1fs err=%v", abs, durationSec, mp3Err)
		out["mp3_error"] = mp3Err.Error()
	} else if mp3Path != "" {
		out["mp3_path"] = mp3Path
		out["mp3_size_bytes"] = mp3Size
		out["mp3_format"] = "mp3"
	}

	detail := recordDetailEnabled()
	if detail {
		pcmStats := recordWAVPCMStatsFromFile(f, size)
		out["rms"] = pcmStats.RMS
		out["peak"] = pcmStats.Peak
		out["header_ok"] = header.OK
		out["channels"] = header.Channels
		out["bits_per_sample"] = header.BitsPerSample

		metaPath := abs + ".meta.json"
		meta := map[string]interface{}{
			"title":           title,
			"safe_title":      safeTitle,
			"path":            abs,
			"file_name":       name,
			"size_bytes":      size,
			"duration_sec":    durationSec,
			"format":          "wav",
			"sample_rate":     sampleRate,
			"channels":        header.Channels,
			"bits_per_sample": header.BitsPerSample,
			"pcm_format":      header.Format,
			"data_bytes":      header.DataBytes,
			"header_ok":       header.OK,
			"rms":             pcmStats.RMS,
			"peak":            pcmStats.Peak,
			"saved_at":        time.Now().Format(time.RFC3339),
			"log_detail":      true,
			"original_path":   abs,
		}
		if mp3Path != "" {
			meta["mp3_path"] = mp3Path
			meta["mp3_size_bytes"] = mp3Size
		}
		if mp3Err != nil {
			meta["mp3_error"] = mp3Err.Error()
		}
		if indexPath, ierr := writeRecordDumpIndex(meta, safeTitle, stamp); ierr != nil {
			log.Printf("[record-audio] dump index failed title=%q err=%v", title, ierr)
		} else {
			meta["debug_dump_index"] = indexPath
			out["debug_dump_path"] = abs
			out["debug_dump_index"] = indexPath
			log.Printf("[record-audio] debug index saved path=%s original=%s", indexPath, abs)
		}
		if metaBytes, merr := json.MarshalIndent(meta, "", "  "); merr != nil {
			log.Printf("[record-audio] meta marshal failed path=%s err=%v", abs, merr)
		} else if werr := os.WriteFile(metaPath, metaBytes, 0o644); werr != nil {
			log.Printf("[record-audio] meta write failed path=%s err=%v", metaPath, werr)
		} else {
			log.Printf("[record-audio] meta saved path=%s", metaPath)
			out["meta_path"] = metaPath
		}
		log.Printf("[record-audio] save ok title=%q path=%s size=%d duration=%.2fs sr=%d rms=%.5f peak=%.5f mp3=%q elapsed=%s",
			title, abs, size, durationSec, sampleRate, pcmStats.RMS, pcmStats.Peak, mp3Path, time.Since(start).Round(time.Millisecond))
	} else {
		// Always emit one ops line even when log-detail is off (product audit trail).
		log.Printf("[record-audio] save ok title=%q path=%s size=%d duration=%.2fs mp3=%q elapsed=%s",
			title, abs, size, durationSec, mp3Path, time.Since(start).Round(time.Millisecond))
	}
	return out, nil
}

// archiveRecordedWAVAsMP3 produces a sibling .mp3 next to the product WAV using the
// built-in pure-Go shine encoder. Returns empty path + error on failure (non-fatal).
func archiveRecordedWAVAsMP3(wavPath string, durationSec float64) (string, int64, error) {
	wavPath = strings.TrimSpace(wavPath)
	if wavPath == "" {
		return "", 0, fmt.Errorf("empty wav path")
	}
	if !tts.HasMP3Encoder() {
		return "", 0, fmt.Errorf("mp3 encoder unavailable")
	}
	// Soft guard before loading multi-hour PCM into memory (encoder bound is 3h).
	if durationSec > float64(3*60*60)+1 {
		return "", 0, fmt.Errorf("duration too long for in-process mp3 archive")
	}
	// Align with product path helper used by post-recording agent context.
	mp3Path := suggestMP3ArchivePath(wavPath)
	if mp3Path == "" {
		return "", 0, fmt.Errorf("empty mp3 path")
	}
	// Bound encode time so a stuck encoder cannot freeze the stop-save UI forever.
	// Scale timeout with duration (min 30s, max 10m) so short clips return quickly
	// and hour-long meetings still have room to finish.
	timeout := 30 * time.Second
	if durationSec > 0 {
		// ~2× realtime is usually enough for shine encode; clamp to [30s, 10m].
		scaled := time.Duration(durationSec * 2 * float64(time.Second))
		if scaled > timeout {
			timeout = scaled
		}
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := tts.EncodeWAVFileToMP3Archive(ctx, wavPath, mp3Path); err != nil {
		_ = os.Remove(mp3Path)
		return "", 0, err
	}
	fi, err := os.Stat(mp3Path)
	if err != nil {
		return "", 0, err
	}
	if fi.Size() <= 0 {
		_ = os.Remove(mp3Path)
		return "", 0, fmt.Errorf("empty mp3 product")
	}
	absMP3, err := filepath.Abs(mp3Path)
	if err != nil {
		absMP3 = mp3Path
	}
	return absMP3, fi.Size(), nil
}

func wavDurationFromSize(size int64, header recordWAVHeaderInfo) float64 {
	if size <= 44 {
		return 0
	}
	dataBytes := size - 44
	sr := header.SampleRate
	if sr <= 0 {
		sr = 16000
	}
	ch := header.Channels
	if ch <= 0 {
		ch = 1
	}
	bits := header.BitsPerSample
	if bits <= 0 {
		bits = 16
	}
	bytesPerSec := int64(sr * ch * (bits / 8))
	if bytesPerSec <= 0 {
		return 0
	}
	return float64(dataBytes) / float64(bytesPerSec)
}

// recordWAVPCMStatsFromFile samples up to ~50k frames via ReadAt (no full mmap).
func recordWAVPCMStatsFromFile(f *os.File, size int64) recordPCMRStats {
	if f == nil || size <= 46 {
		return recordPCMRStats{}
	}
	dataBytes := size - 44
	if dataBytes < 2 {
		return recordPCMRStats{}
	}
	if dataBytes%2 != 0 {
		dataBytes--
	}
	sampleCount := dataBytes / 2
	const maxSamples = 50000
	stride := int64(1)
	if sampleCount > maxSamples {
		stride = sampleCount / maxSamples
	}
	var sumSq float64
	var peak float64
	n := 0
	buf := make([]byte, 2)
	for i := int64(0); i < sampleCount; i += stride {
		off := 44 + i*2
		if _, err := f.ReadAt(buf, off); err != nil {
			break
		}
		s := int16(uint16(buf[0]) | uint16(buf[1])<<8)
		fv := float64(s) / 32768.0
		if fv < 0 {
			fv = -fv
		}
		if fv > peak {
			peak = fv
		}
		sumSq += fv * fv
		n++
	}
	if n == 0 {
		return recordPCMRStats{}
	}
	return recordPCMRStats{RMS: math.Sqrt(sumSq / float64(n)), Peak: peak}
}

// recordDetailEnabled gates detailed record-audio logs and raw voice dumps.
// Bound to the app-wide settings switch「日志详情」(log_detail_enabled).
func recordDetailEnabled() bool {
	return corelib.IsLogDetailEnabled()
}

// writeRecordDumpIndex writes a small JSON index under ~/.maclaw/record_dump/
// pointing at the product WAV (no second full copy of multi-hour audio).
func writeRecordDumpIndex(meta map[string]interface{}, safeTitle, stamp string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".maclaw", "record_dump")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	n := atomic.AddInt64(&recordDumpCounter, 1)
	if safeTitle == "" {
		safeTitle = "recording"
	}
	safeStamp := strings.ReplaceAll(stamp, ":", "")
	filename := fmt.Sprintf("record_%s_%s_%03d.json", safeStamp, safeTitle, n%1000)
	path := filepath.Join(dir, filename)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// putLE32u writes v as little-endian uint32 at b[off:].
func putLE32u(b []byte, off int, v uint32) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
	b[off+2] = byte(v >> 16)
	b[off+3] = byte(v >> 24)
}

// makeWAVHeaderBytes builds a 44-byte mono PCM16 LE WAV header.
func makeWAVHeaderBytes(dataBytes int64, sampleRate int) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if dataBytes < 0 {
		dataBytes = 0
	}
	// RIFF size field is uint32; clamp to avoid wrap on pathological lengths.
	const maxU32 = int64(^uint32(0))
	riffSize := int64(36) + dataBytes
	if riffSize > maxU32 {
		riffSize = maxU32
	}
	db := dataBytes
	if db > maxU32 {
		db = maxU32
	}
	b := make([]byte, 44)
	copy(b[0:], []byte("RIFF"))
	putLE32u(b, 4, uint32(riffSize))
	copy(b[8:], []byte("WAVE"))
	copy(b[12:], []byte("fmt "))
	putLE32u(b, 16, 16) // PCM fmt chunk size
	// format=1 PCM, channels=1, sampleRate, byteRate=sr*2, blockAlign=2, bits=16
	b[20], b[21] = 1, 0
	b[22], b[23] = 1, 0
	putLE32u(b, 24, uint32(sampleRate))
	putLE32u(b, 28, uint32(sampleRate*2))
	b[32], b[33] = 2, 0
	b[34], b[35] = 16, 0
	copy(b[36:], []byte("data"))
	putLE32u(b, 40, uint32(db))
	return b
}

type recordWAVHeaderInfo struct {
	OK            bool
	Format        int
	Channels      int
	SampleRate    int
	BitsPerSample int
	DataBytes     int
}

func inspectRecordWAVHeader(wav []byte) recordWAVHeaderInfo {
	info := recordWAVHeaderInfo{}
	if len(wav) < 44 {
		return info
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return info
	}
	info.Format = int(uint16(wav[20]) | uint16(wav[21])<<8)
	info.Channels = int(uint16(wav[22]) | uint16(wav[23])<<8)
	info.SampleRate = int(uint32(wav[24]) | uint32(wav[25])<<8 | uint32(wav[26])<<16 | uint32(wav[27])<<24)
	info.BitsPerSample = int(uint16(wav[34]) | uint16(wav[35])<<8)
	info.DataBytes = int(uint32(wav[40]) | uint32(wav[41])<<8 | uint32(wav[42])<<16 | uint32(wav[43])<<24)
	info.OK = info.Format == 1 && info.Channels > 0 && info.SampleRate > 0 && info.BitsPerSample > 0
	return info
}

// validateRecordedWAV enforces the contract produced by the desktop recorder:
// PCM, 1–2 ch, 8–48 kHz, 16-bit, data payload present.
func validateRecordedWAV(h recordWAVHeaderInfo, totalBytes int) error {
	if !h.OK {
		return fmt.Errorf("invalid or unsupported WAV header")
	}
	if h.Format != 1 {
		return fmt.Errorf("only PCM WAV is supported (format=%d)", h.Format)
	}
	if h.Channels < 1 || h.Channels > 2 {
		return fmt.Errorf("unsupported channel count %d", h.Channels)
	}
	if h.SampleRate < 8000 || h.SampleRate > 48000 {
		return fmt.Errorf("unsupported sample rate %d", h.SampleRate)
	}
	if h.BitsPerSample != 16 {
		return fmt.Errorf("only 16-bit PCM is supported (bits=%d)", h.BitsPerSample)
	}
	if h.DataBytes <= 0 {
		return fmt.Errorf("empty WAV data chunk")
	}
	if h.DataBytes > totalBytes-44 {
		if totalBytes <= 44 {
			return fmt.Errorf("WAV payload missing")
		}
	}
	return nil
}

type recordPCMRStats struct {
	RMS  float64
	Peak float64
}

// recordWAVPCMStatsSampled estimates energy on 16-bit PCM without scanning every
// sample of multi-hour recordings (stride grows with length).
func recordWAVPCMStatsSampled(wav []byte) recordPCMRStats {
	if len(wav) < 46 {
		return recordPCMRStats{}
	}
	data := wav[44:]
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	if len(data) < 2 {
		return recordPCMRStats{}
	}
	const maxSamples = 50000
	sampleCount := len(data) / 2
	stride := 1
	if sampleCount > maxSamples {
		stride = sampleCount / maxSamples
	}
	var sumSq float64
	var peak float64
	n := 0
	for i := 0; i+1 < len(data); i += 2 * stride {
		s := int16(uint16(data[i]) | uint16(data[i+1])<<8)
		f := float64(s) / 32768.0
		if f < 0 {
			f = -f
		}
		if f > peak {
			peak = f
		}
		sumSq += f * f
		n++
	}
	if n == 0 {
		return recordPCMRStats{}
	}
	return recordPCMRStats{RMS: math.Sqrt(sumSq / float64(n)), Peak: peak}
}

func sanitizeRecordingFileTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "recording"
	}
	var b strings.Builder
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '·':
			b.WriteRune('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "recording"
	}
	return out
}

func wavSampleRate(wav []byte) int {
	h := inspectRecordWAVHeader(wav)
	if h.SampleRate > 0 {
		return h.SampleRate
	}
	return 16000
}

func wavDurationSeconds(wav []byte, header recordWAVHeaderInfo) float64 {
	if len(wav) < 44 {
		return 0
	}
	dataBytes := header.DataBytes
	if dataBytes <= 0 || dataBytes > len(wav)-44 {
		dataBytes = len(wav) - 44
	}
	sr := header.SampleRate
	if sr <= 0 {
		sr = wavSampleRate(wav)
	}
	ch := header.Channels
	if ch <= 0 {
		ch = 1
	}
	bits := header.BitsPerSample
	if bits <= 0 {
		bits = 16
	}
	bytesPerSec := sr * ch * (bits / 8)
	if bytesPerSec <= 0 {
		return 0
	}
	return float64(dataBytes) / float64(bytesPerSec)
}
