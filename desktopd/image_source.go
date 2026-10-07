package desktopd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultImageSource is the public build of desktopd/image/Dockerfile.v2
// (.github/workflows/desktop-image.yml). When maclaw-gui:2 is missing on the
// Docker host, desktopd pulls this reference and tags it maclaw-gui:2.
const DefaultImageSource = "ghcr.io/rapidai/maclaw-gui:2"

// DefaultImagePullTimeout bounds one source pull. The image is ~800 MB
// compressed; slow uplinks (mainland China to ghcr.io) need the margin.
const DefaultImagePullTimeout = 30 * time.Minute

// imagePullRetryAfter keeps a failed source pull from being retried on every
// desktop request; the next attempt after it starts a fresh pull.
const imagePullRetryAfter = time.Minute

// imageContractScript is the check desktopd/remote_deploy.sh runs on a fresh
// build: the files and tools desktop_supervisor.py and desktopd rely on. A
// pulled image that fails it is never tagged as the local image.
const imageContractScript = `test -f /desktop_supervisor.py && test -f /usr/share/novnc/vnc.html && ` +
	`for tool in python3 Xvfb x11vnc websockify xdotool chromium startxfce4 dbus-launch import; do ` +
	`command -v "$tool" >/dev/null || { echo "missing $tool"; exit 1; }; done`

// ParseImageSources reads DESKTOPD_IMAGE_SOURCE:
//
//	""                         -> maclaw-gui:2 from DefaultImageSource
//	"off" / "none" / "0"       -> no source pulls (build the image on the host)
//	"<ref>"                    -> maclaw-gui:2 from <ref>
//	"<local>=<ref>[,...]"      -> each local image name from its own ref
//
// A ref may pin a digest: ghcr.io/rapidai/maclaw-gui@sha256:<digest> (or
// name:tag@sha256:<digest>); docker then verifies the content it pulls.
func ParseImageSources(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	switch strings.ToLower(raw) {
	case "":
		return map[string]string{DefaultImage: DefaultImageSource}, nil
	case "off", "none", "0", "false", "disabled":
		return nil, nil
	}
	out := map[string]string{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		local, ref := DefaultImage, item
		if i := strings.Index(item, "="); i >= 0 {
			local, ref = strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		}
		if !validImageRef(local) || !validImageRef(ref) {
			return nil, fmt.Errorf("%w: invalid image source %q", ErrInvalid, item)
		}
		if strings.Contains(local, "@") {
			return nil, fmt.Errorf("%w: local image %q must be a name, not a digest", ErrInvalid, local)
		}
		out[local] = ref
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// validImageRef rejects values docker would read as flags or that could not
// be one image reference.
func validImageRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || len(ref) > 512 {
		return false
	}
	for _, r := range ref {
		if r <= ' ' || r == 0x7f || r == '=' || r == ',' {
			return false
		}
	}
	return true
}

// ImagePullStatus is what the admin panel shows per configured source.
type ImagePullStatus struct {
	Image   string `json:"image"`
	Source  string `json:"source"`
	State   string `json:"state"` // idle | pulling | pulled | failed
	Message string `json:"message,omitempty"`
	At      string `json:"at,omitempty"`
}

type imagePull struct {
	done   chan struct{}
	err    error
	ended  time.Time
	status ImagePullStatus
}

type imagePulls struct {
	mu    sync.Mutex
	calls map[string]*imagePull
}

func (s *Service) imageSource(image string) (string, bool) {
	ref, ok := s.ImageSources[image]
	return ref, ok && ref != ""
}

func (s *Service) pullTimeout() time.Duration {
	if s.ImagePullTimeout > 0 {
		return s.ImagePullTimeout
	}
	return DefaultImagePullTimeout
}

func (s *Service) imagePresent(ctx context.Context, image string) bool {
	_, err := s.docker(ctx, "image", "inspect", "--format", "{{.Id}}", image)
	return err == nil
}

// installImage makes sure the requested image exists before a desktop is
// (re)created from it. An image that is present is used as is: desktopd never
// pulls over or replaces a local image. A missing image with a configured
// source is pulled from that source, checked, and tagged with the local name.
// Images without a source keep the historical plain `docker pull`.
func (s *Service) installImage(ctx context.Context, image string) error {
	if s.imagePresent(ctx, image) {
		return nil
	}
	source, ok := s.imageSource(image)
	if !ok {
		_, err := s.docker(ctx, "pull", image)
		return err
	}
	call := s.startImagePull(image, source)
	select {
	case <-call.done:
		return call.err
	case <-ctx.Done():
		// The pull keeps running in the background; the next request uses it.
		return fmt.Errorf("%w: image %s is missing and is still being pulled from %s; retry in a few minutes", ErrDocker, image, source)
	}
}

// PrefetchImages pulls every configured source whose local image is missing.
// It runs in the background at startup and never blocks the HTTP server.
func (s *Service) PrefetchImages(ctx context.Context) {
	for image, source := range s.ImageSources {
		if source == "" {
			continue
		}
		if s.imagePresent(ctx, image) {
			log.Printf("[desktopd] image %s is present; not pulling %s", image, source)
			continue
		}
		call := s.startImagePull(image, source)
		select {
		case <-call.done:
		case <-ctx.Done():
			return
		}
	}
}

// startImagePull joins the pull already running for image or starts one. A
// failure is remembered for imagePullRetryAfter so a down registry is not
// hit on every desktop request.
func (s *Service) startImagePull(image, source string) *imagePull {
	s.pulls.mu.Lock()
	defer s.pulls.mu.Unlock()
	if s.pulls.calls == nil {
		s.pulls.calls = map[string]*imagePull{}
	}
	if call := s.pulls.calls[image]; call != nil {
		select {
		case <-call.done:
			if call.err != nil && time.Since(call.ended) < imagePullRetryAfter {
				return call
			}
		default:
			return call
		}
	}
	call := &imagePull{done: make(chan struct{}), status: ImagePullStatus{
		Image: image, Source: source, State: "pulling", At: time.Now().Format(time.RFC3339),
	}}
	s.pulls.calls[image] = call
	go func() {
		err := s.pullImageFromSource(image, source)
		s.pulls.mu.Lock()
		call.err, call.ended = err, time.Now()
		call.status.At = call.ended.Format(time.RFC3339)
		if err != nil {
			call.status.State, call.status.Message = "failed", err.Error()
		} else {
			call.status.State = "pulled"
		}
		s.pulls.mu.Unlock()
		close(call.done)
	}()
	return call
}

func (s *Service) pullImageFromSource(image, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.pullTimeout())
	defer cancel()
	started := time.Now()
	log.Printf("[desktopd] image %s is missing; pulling %s (timeout %s)", image, source, s.pullTimeout())
	fail := func(step string, err error) error {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			step += " (timed out)"
		}
		log.Printf("[desktopd] %s for %s failed after %s: %v; build it on this host instead "+
			"(desktopd/remote_deploy.sh, or DESKTOPD_IMAGE_SOURCE=off to stop pulling)", step, image, time.Since(started).Round(time.Second), err)
		return fmt.Errorf("%w: image %s is missing and %s from %s failed: %v", ErrDocker, image, step, source, err)
	}
	if _, err := s.docker(ctx, "pull", source); err != nil {
		return fail("pull", err)
	}
	id, err := s.docker(ctx, "image", "inspect", "--format", "{{.Id}}", source)
	if err != nil || strings.TrimSpace(id) == "" {
		if err == nil {
			err = errors.New("pulled image has no id")
		}
		return fail("inspect", err)
	}
	id = strings.TrimSpace(id)
	if out, err := s.docker(ctx, "run", "--rm", "--entrypoint", "sh", id, "-c", imageContractScript); err != nil {
		if strings.TrimSpace(out) != "" {
			err = fmt.Errorf("%v (%s)", err, strings.TrimSpace(out))
		}
		return fail("contract check", err)
	}
	// Someone may have built or loaded the image while this pull ran. Never
	// replace a local image: the operator's copy wins.
	if s.imagePresent(ctx, image) {
		log.Printf("[desktopd] image %s appeared while pulling %s; keeping the local image", image, source)
		return nil
	}
	if _, err := s.docker(ctx, "tag", id, image); err != nil {
		return fail("tag", err)
	}
	log.Printf("[desktopd] image %s ready from %s (%s) in %s", image, source, id, time.Since(started).Round(time.Second))
	return nil
}

// ImagePullStatuses reports each configured source for the admin panel.
func (s *Service) ImagePullStatuses() []ImagePullStatus {
	s.pulls.mu.Lock()
	defer s.pulls.mu.Unlock()
	out := make([]ImagePullStatus, 0, len(s.ImageSources))
	for image, source := range s.ImageSources {
		status := ImagePullStatus{Image: image, Source: source, State: "idle"}
		if call := s.pulls.calls[image]; call != nil {
			status = call.status
		}
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Image < out[j].Image })
	return out
}
