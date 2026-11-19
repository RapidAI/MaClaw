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
	"sync"
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
	if spec.Size == 0 {
		// Do not open the partial file first. A missing size used to truncate
		// and delete the bytes a real download was resuming.
		return fetchEmpty(ctx, fetch, folder, spec)
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
	for attempt := 0; done < spec.Size; attempt++ {
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

// fetchEmpty stores a file whose declared size is zero. A body with bytes is
// refused before any partial file is opened, so a resume already on disk
// stays intact.
func fetchEmpty(ctx context.Context, fetch Fetcher, folder string, spec FetchSpec) (string, error) {
	url := spec.URL
	refreshed := false
	for {
		resp, err := fetch(ctx, url, 0)
		if err != nil {
			return "", &Err{Code: CodeFailed, Message: err.Error()}
		}
		code := resp.StatusCode
		if code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusGone {
			resp.Body.Close()
			if spec.Refresh != nil && !refreshed {
				refreshed = true
				if next, rerr := spec.Refresh(ctx); rerr == nil && strings.TrimSpace(next) != "" {
					url = next
					continue
				}
			}
			if code == http.StatusGone {
				return "", &Err{Code: CodeExpired, Message: "the file is no longer kept on the server (it expired); ask the bot to send it again"}
			}
			return "", &Err{Code: CodeDenied, Message: fmt.Sprintf("%v (HTTP %d)", ErrFetchAuth, code)}
		}
		if code == http.StatusNotFound {
			resp.Body.Close()
			return "", &Err{Code: CodeNotFound, Message: "the file is not on the server any more; ask the bot to send it again"}
		}
		if code != http.StatusOK && code != http.StatusNoContent {
			resp.Body.Close()
			return "", &Err{Code: CodeFailed, Message: fmt.Sprintf("HTTP %d", code)}
		}
		var peek [1]byte
		var n int
		var rerr error
		if resp.Body != nil {
			n, rerr = resp.Body.Read(peek[:])
			resp.Body.Close()
		}
		if n > 0 {
			return "", &Err{Code: CodeInvalid, Message: "the download was missing a file size"}
		}
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return "", &Err{Code: CodeFailed, Message: rerr.Error()}
		}
		break
	}
	name := SafeName(spec.Name)
	if name == "" {
		name = "file"
	}
	f, final, err := CreateUnique(folder, name)
	if err != nil {
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	if err := f.Close(); err != nil {
		os.Remove(final)
		return "", &Err{Code: CodeFailed, Message: err.Error()}
	}
	if want := askedSHA(spec); want != "" {
		sum := sha256.Sum256(nil)
		if hex.EncodeToString(sum[:]) != want {
			os.Remove(final)
			return "", &Err{Code: CodeFailed, Message: "the downloaded file did not match the server's checksum and was discarded; try again"}
		}
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
// The chat message is already that file, so the save does not report a
// transfer. A second save of the same stored file into the same folder,
// including one that asks for a different name, waits for that download
// and gets the same path.
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
	return a.fetchInto(ctx, folder, botID, botName, runID, "", spec, fetchQuiet)
}

// fetchShown reports progress to the person's computer. fetchQuiet is a
// chat attachment being stored so the message can open it: that save is not
// a transfer and must not grow a download chip.
type fetchVisibility bool

const (
	fetchQuiet fetchVisibility = false
	fetchShown fetchVisibility = true
)

// fetchFlight is one download of one partial file. The map is process-wide:
// a chat save and a relay transfer can run on different Agent values and
// must still share the partial file instead of writing it together.
type fetchFlight struct {
	done    chan struct{}
	path    string
	err     error
	waiters int
	size    int64
	sha     string
	eventID string
}

var fetchFlights struct {
	sync.Mutex
	m map[string]*fetchFlight
}

func fetchFlightKey(folder string, spec FetchSpec) string {
	return filepath.Join(filepath.Clean(folder), partName(spec))
}

func askedSHA(spec FetchSpec) string {
	return strings.ToLower(strings.TrimSpace(spec.SHA256))
}

func shaMismatch(want, have string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	have = strings.ToLower(strings.TrimSpace(have))
	return want != "" && have != "" && want != have
}

func (a *Agent) fetchInto(ctx context.Context, folder, botID, botName, runID, remote string, spec FetchSpec, visibility fetchVisibility) (string, error) {
	if spec.Refresh == nil && spec.Ref != "" && a.RefreshLink != nil {
		ref := spec.Ref
		spec.Refresh = func(ctx context.Context) (string, error) { return a.RefreshLink(ctx, ref) }
	}
	key := fetchFlightKey(folder, spec)
	for {
		fetchFlights.Lock()
		if fetchFlights.m == nil {
			fetchFlights.m = map[string]*fetchFlight{}
		}
		flight := fetchFlights.m[key]
		if flight == nil {
			flight = &fetchFlight{done: make(chan struct{}), size: spec.Size, sha: askedSHA(spec)}
			if visibility == fetchShown {
				flight.eventID = firstNonEmpty(spec.Ref, newID())
			}
			fetchFlights.m[key] = flight
			fetchFlights.Unlock()
			return a.leadFetch(ctx, key, flight, folder, botID, botName, runID, remote, spec, visibility)
		}
		select {
		case <-flight.done:
			path, ferr, sha, size := flight.path, flight.err, flight.sha, flight.size
			fetchFlights.Unlock()
			if ferr == nil {
				use, sum, cerr := classifyCached(path, spec, size, sha)
				if cerr != nil {
					return "", cerr
				}
				if use {
					rememberFlightSHA(flight, sum)
					return path, nil
				}
			}
			fetchFlights.Lock()
			if fetchFlights.m[key] == flight {
				delete(fetchFlights.m, key)
			}
			fetchFlights.Unlock()
		default:
			flight.waiters++
			sha, size, eventID := flight.sha, flight.size, flight.eventID
			fetchFlights.Unlock()
			path, err := waitFetch(ctx, flight)
			if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				return "", &Err{Code: CodeFailed, Message: "the download was stopped; it resumes next time"}
			}
			// The leader's own cancel must not fail a caller that is still
			// waiting. The partial file is still there; this caller resumes it.
			if err != nil && ctx.Err() == nil && downloadStopped(err) {
				continue
			}
			if err == nil {
				use, sum, cerr := classifyCached(path, spec, size, sha)
				if cerr != nil {
					err = cerr
					path = ""
				} else if !use {
					continue
				} else {
					rememberFlightSHA(flight, sum)
				}
			}
			// The leader publishes the chip while bytes are moving. A quiet
			// leader does not, so an explicit transfer that joined it reports
			// the outcome once.
			if visibility == fetchShown && eventID == "" {
				a.reportFetch("", botID, botName, runID, remote, spec, path, err)
			}
			return path, err
		}
	}
}

func (a *Agent) leadFetch(ctx context.Context, key string, flight *fetchFlight, folder, botID, botName, runID, remote string, spec FetchSpec, visibility fetchVisibility) (path string, err error) {
	finished := false
	defer func() {
		if finished {
			return
		}
		if flight.err == nil {
			flight.err = &Err{Code: CodeFailed, Message: "the download failed"}
		}
		select {
		case <-flight.done:
		default:
			close(flight.done)
		}
		fetchFlights.Lock()
		if fetchFlights.m[key] == flight {
			delete(fetchFlights.m, key)
		}
		fetchFlights.Unlock()
	}()
	path, err = a.downloadFetch(ctx, folder, botID, botName, runID, remote, spec, flight.eventID, visibility)
	flight.path, flight.err = path, err
	finished = true
	close(flight.done)
	if err != nil {
		fetchFlights.Lock()
		if fetchFlights.m[key] == flight {
			delete(fetchFlights.m, key)
		}
		fetchFlights.Unlock()
	}
	return path, err
}

func downloadStopped(err error) bool {
	return err != nil && strings.Contains(err.Error(), "the download was stopped")
}

// classifyCached says whether path can answer spec. A checksum or a size
// that belongs to a different completed save is an error and leaves that
// save in place. A missing file, or a file that is not the size that was
// saved, is not usable and the caller downloads again.
func classifyCached(path string, spec FetchSpec, savedSize int64, savedSHA string) (use bool, sum string, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		return false, "", nil
	}
	if spec.Size > 0 && info.Size() != spec.Size {
		if savedSize > 0 && info.Size() == savedSize {
			return false, "", &Err{Code: CodeFailed, Message: "the saved file is a different size than was asked for"}
		}
		return false, "", nil
	}
	if shaMismatch(spec.SHA256, savedSHA) {
		return false, "", &Err{Code: CodeFailed, Message: "the saved file does not match the checksum that was asked for"}
	}
	asked := askedSHA(spec)
	if asked != "" && savedSHA == "" {
		got, herr := fileSHA256(path)
		if herr != nil {
			return false, "", &Err{Code: CodeFailed, Message: herr.Error()}
		}
		if got != asked {
			return false, "", &Err{Code: CodeFailed, Message: "the saved file does not match the checksum that was asked for"}
		}
		return true, got, nil
	}
	return true, "", nil
}

func rememberFlightSHA(flight *fetchFlight, sum string) {
	if flight == nil || sum == "" {
		return
	}
	fetchFlights.Lock()
	if flight.sha == "" {
		flight.sha = sum
	}
	fetchFlights.Unlock()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func waitFetch(ctx context.Context, flight *fetchFlight) (string, error) {
	select {
	case <-flight.done:
		return flight.path, flight.err
	case <-ctx.Done():
		select {
		case <-flight.done:
			return flight.path, flight.err
		default:
			return "", ctx.Err()
		}
	}
}

func (a *Agent) downloadFetch(ctx context.Context, folder, botID, botName, runID, remote string, spec FetchSpec, eventID string, visibility fetchVisibility) (string, error) {
	if visibility != fetchShown {
		return FetchToFolder(ctx, a.Fetch, folder, spec, nil)
	}
	ev := TransferEvent{ID: firstNonEmpty(eventID, spec.Ref, newID()), BotID: botID, BotName: botName, RunID: runID, Direction: "to_local", Name: SafeName(spec.Name), Remote: remote, Size: spec.Size, State: "running"}
	send := func() { a.notify(ev) }
	send()
	path, err := FetchToFolder(ctx, a.Fetch, folder, spec, func(done int64) {
		ev.Done = done
		send()
	})
	if err != nil {
		ev.State, ev.Error = "failed", err.Error()
	} else {
		ev.State, ev.Done, ev.LocalPath, ev.Name = "done", spec.Size, path, filepath.Base(path)
	}
	send()
	return path, err
}

func (a *Agent) reportFetch(eventID, botID, botName, runID, remote string, spec FetchSpec, path string, err error) {
	ev := TransferEvent{ID: firstNonEmpty(eventID, spec.Ref, newID()), BotID: botID, BotName: botName, RunID: runID, Direction: "to_local", Name: SafeName(spec.Name), Remote: remote, Size: spec.Size}
	if err != nil {
		ev.State, ev.Error = "failed", err.Error()
	} else {
		ev.State, ev.Done, ev.LocalPath, ev.Name = "done", spec.Size, path, filepath.Base(path)
	}
	a.notify(ev)
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
	path, err := a.fetchInto(context.WithoutCancel(ctx), d.Resolved, req.BotID, req.BotName, req.RunID, req.Remote, FetchSpec{URL: req.URL, Ref: req.Ref, Name: name, Size: req.Size, SHA256: req.SHA256}, fetchShown)
	if err != nil {
		respondErr(stream, err)
		return nil
	}
	_ = WriteLine(stream, Response{OK: true, Path: path, Name: filepath.Base(path), Size: req.Size, State: "done"})
	return nil
}
