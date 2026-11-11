package localrelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Large files reach the person's computer from Hub's file store over plain
// HTTP: the computer pulls them with Range requests, so a broken connection
// resumes where it stopped, and a file is never inlined in a chat message.

// Fetcher GETs url (Hub-relative "/api/..." or absolute) from byte offset.
// The GUI supplies one that carries its machine credential.
type Fetcher func(ctx context.Context, url string, offset int64) (*http.Response, error)

// FetchSpec is one stored file to download.
type FetchSpec struct {
	URL    string
	Ref    string
	Name   string
	Size   int64
	SHA256 string
	// Refresh returns a new URL when the signed one expired; may be nil.
	Refresh func(ctx context.Context) (string, error)
}

// ErrFetchAuth is a refused download (expired link, wrong owner, gone).
var ErrFetchAuth = errors.New("the server refused the download link")

const fetchAttempts = 8

// partName is the resumable partial file for one stored file. It starts
// with a dot so a half-done download does not look like the real file.
func partName(spec FetchSpec) string {
	key := SafeName(spec.Ref)
	if key == "" {
		sum := sha256.Sum256([]byte(spec.URL + "|" + spec.Name))
		key = hex.EncodeToString(sum[:8])
	}
	return ".maclaw-" + key + ".part"
}

// FetchToFolder downloads spec into folder and returns the saved path. The
// final name is new ("name (1).ext" when taken); nothing is overwritten. A
// partial file from an earlier attempt is resumed. The size and, when
// given, the SHA-256 are checked before the file gets its real name.
func FetchToFolder(ctx context.Context, fetch Fetcher, folder string, spec FetchSpec, progress func(done int64)) (string, error) {
	if fetch == nil {
		return "", &Err{Code: CodeFailed, Message: "this computer cannot download from the server"}
	}
	if spec.Size < 0 || strings.TrimSpace(spec.URL) == "" {
		return "", &Err{Code: CodeInvalid, Message: "the download link is missing"}
	}
	name := SafeName(spec.Name)
	if name == "" {
		name = "file"
	}
	part := filepath.Join(folder, partName(spec))
	f, err := os.OpenFile(part, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	done, _ := f.Seek(0, io.SeekEnd)
	if done > spec.Size {
		_ = f.Truncate(0)
		done, _ = f.Seek(0, io.SeekStart)
	}
	if progress != nil {
		progress(done)
	}
	url := spec.URL
	refreshed := false
	var lastErr error
	for attempt := 0; done < spec.Size || (spec.Size == 0 && attempt == 0); attempt++ {
		if attempt >= fetchAttempts {
			break
		}
		if attempt > 0 {
			wait := time.Duration(1<<min(attempt-1, 4)) * time.Second
			select {
			case <-ctx.Done():
				f.Close()
				return "", &Err{Code: CodeFailed, Message: "the download was stopped; it resumes next time"}
			case <-time.After(wait):
			}
		}
		resp, err := fetch(ctx, url, done)
		if err != nil {
			lastErr = err
			continue
		}
		switch {
		case resp.StatusCode == http.StatusPartialContent || (resp.StatusCode == http.StatusOK && done == 0):
		case resp.StatusCode == http.StatusOK:
			// The server ignored Range: start over.
			_ = f.Truncate(0)
			done, _ = f.Seek(0, io.SeekStart)
		case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && done == spec.Size:
			resp.Body.Close()
			continue
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone:
			resp.Body.Close()
			if spec.Refresh != nil && !refreshed {
				refreshed = true
				if next, rerr := spec.Refresh(ctx); rerr == nil && next != "" {
					url = next
					attempt--
					continue
				}
			}
			f.Close()
			if resp.StatusCode == http.StatusGone {
				os.Remove(part)
				return "", &Err{Code: CodeExpired, Message: "the file is no longer kept on the server (it expired); ask the bot to send it again"}
			}
			return "", &Err{Code: CodeDenied, Message: fmt.Sprintf("%v (HTTP %d)", ErrFetchAuth, resp.StatusCode)}
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			f.Close()
			os.Remove(part)
			return "", &Err{Code: CodeNotFound, Message: "the file is not on the server any more; ask the bot to send it again"}
		default:
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if _, err := f.Seek(done, io.SeekStart); err != nil {
			resp.Body.Close()
			f.Close()
			return "", &Err{Code: CodeFailed, Message: err.Error()}
		}
		w := &progressWriter{w: f, done: &done, progress: progress}
		_, cerr := io.Copy(w, io.LimitReader(resp.Body, spec.Size-done))
		resp.Body.Close()
		if cerr != nil {
			lastErr = cerr
			if ctx.Err() != nil {
				f.Close()
				return "", &Err{Code: CodeFailed, Message: "the download was stopped; it resumes next time"}
			}
			continue
		}
		if progress != nil {
			progress(done)
		}
		if spec.Size == 0 {
			break
		}
	}
	if done != spec.Size {
		f.Close()
		msg := fmt.Sprintf("downloaded %s of %s; the rest resumes next time", HumanSize(done), HumanSize(spec.Size))
		if lastErr != nil {
			msg += " (" + lastErr.Error() + ")"
		}
		return "", &Err{Code: CodeFailed, Message: msg}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	if want := strings.ToLower(strings.TrimSpace(spec.SHA256)); want != "" {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return "", &Err{Code: CodeFailed, Message: err.Error()}
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			f.Close()
			return "", &Err{Code: CodeFailed, Message: err.Error()}
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != want {
			f.Close()
			os.Remove(part)
			return "", &Err{Code: CodeFailed, Message: "the downloaded file did not match the server's checksum and was discarded; try again"}
		}
	}
	f.Close()
	reserved, final, err := CreateUnique(folder, name)
	if err != nil {
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	reserved.Close()
	// The reserved empty file is ours; the finished part takes its place.
	if err := os.Rename(part, final); err != nil {
		os.Remove(final)
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	return final, nil
}

type progressWriter struct {
	w        io.Writer
	done     *int64
	progress func(int64)
	last     time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	*p.done += int64(n)
	if p.progress != nil && time.Since(p.last) > 400*time.Millisecond {
		p.last = time.Now()
		p.progress(*p.done)
	}
	return n, err
}

// HTTPFetcher returns a Fetcher for base (Hub's URL) that sets header on
// every request.
func HTTPFetcher(client *http.Client, base string, header func(*http.Request)) Fetcher {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 60 * time.Second, IdleConnTimeout: 90 * time.Second}}
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	return func(ctx context.Context, url string, offset int64) (*http.Response, error) {
		if strings.HasPrefix(url, "/") {
			url = base + url
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if header != nil {
			header(req)
		}
		if offset > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
		}
		return client.Do(req)
	}
}

// SaveDelivered downloads a file a bot delivered in chat into that bot's
// own folder on this computer (the person's chat, so no question is asked).
// Concurrent calls for one file share the download.
func (a *Agent) SaveDelivered(ctx context.Context, botID, botName, runID string, spec FetchSpec) (string, error) {
	s := a.settings()
	folder := s.BotFolder(botName, botID)
	if folder == "" && a.Home != "" {
		name := firstNonEmpty(SafeName(botName), SafeName(botID), "bot")
		folder = filepath.Join(a.Home, BaseFolder, name)
	}
	if folder == "" {
		return "", &Err{Code: CodeFailed, Message: "no folder to save into"}
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	return a.fetchInto(ctx, folder, botID, botName, runID, "", spec)
}

func (a *Agent) fetchInto(ctx context.Context, folder, botID, botName, runID, remote string, spec FetchSpec) (string, error) {
	if spec.Refresh == nil && spec.Ref != "" && a.RefreshLink != nil {
		ref := spec.Ref
		spec.Refresh = func(ctx context.Context) (string, error) { return a.RefreshLink(ctx, ref) }
	}
	l := a.keyLock("fetch|" + firstNonEmpty(spec.Ref, spec.URL))
	l.Lock()
	defer l.Unlock()
	ev := TransferEvent{ID: firstNonEmpty(spec.Ref, newID()), BotID: botID, BotName: botName, RunID: runID, Direction: "to_local", Name: SafeName(spec.Name), Remote: remote, Size: spec.Size, State: "running"}
	a.notify(ev)
	path, err := FetchToFolder(ctx, a.Fetch, folder, spec, func(done int64) {
		ev.Done = done
		a.notify(ev)
	})
	if err != nil {
		ev.State, ev.Error = "failed", err.Error()
		a.notify(ev)
		return "", err
	}
	ev.State, ev.Done, ev.LocalPath, ev.Name = "done", spec.Size, path, filepath.Base(path)
	a.notify(ev)
	return path, nil
}

// fetch answers OpFetch: approve the folder like a write, answer, download,
// answer again with the saved path.
func (a *Agent) fetch(ctx context.Context, stream net.Conn, s Settings, req Request) error {
	folder, err := a.writeFolder(s, req)
	if err != nil {
		return err
	}
	d, err := s.CheckPath(folder, a.Home, AccessWrite)
	if err != nil {
		return err
	}
	if info, err := os.Stat(d.Resolved); err != nil || !info.IsDir() {
		return &Err{Code: CodeInvalid, Message: d.Resolved + " is not a folder"}
	}
	name := SafeName(req.Name)
	if name == "" {
		return &Err{Code: CodeInvalid, Message: "the file name is empty or invalid"}
	}
	if req.Size < 0 || req.Size > s.MaxBytes() {
		return &Err{Code: CodeTooLarge, Message: fmt.Sprintf("the file is %s, above the %d MB limit set on this computer", HumanSize(req.Size), s.MaxTransferMB)}
	}
	if a.Fetch == nil {
		return &Err{Code: CodeFailed, Message: "this MaClaw app cannot download from the server; update it"}
	}
	if err := a.approve(ctx, s, req.BotID, d.Class, Prompt{BotID: req.BotID, BotName: req.BotName, Kind: "write", Target: filepath.Join(d.Resolved, name), Remote: req.Remote, Outside: d.Outside, Size: req.Size}); err != nil {
		return err
	}
	if err := WriteLine(stream, Response{OK: true, Path: d.Resolved, Name: name, State: "running"}); err != nil {
		return nil
	}
	// The download belongs to the computer now: it goes on even when Hub
	// stops waiting for the answer.
	path, err := a.fetchInto(context.WithoutCancel(ctx), d.Resolved, req.BotID, req.BotName, req.RunID, req.Remote, FetchSpec{URL: req.URL, Ref: req.Ref, Name: name, Size: req.Size, SHA256: req.SHA256})
	if err != nil {
		respondErr(stream, err)
		return nil
	}
	_ = WriteLine(stream, Response{OK: true, Path: path, Name: filepath.Base(path), Size: req.Size, State: "done"})
	return nil
}
