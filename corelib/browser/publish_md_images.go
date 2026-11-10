package browser

// Pictures into a markdown editor (CSDN editor.csdn.net/md): the editor
// uploads a pasted picture itself and writes its own ![..](https://..) link.
// Pasting the next picture while one is still uploading lost pictures and
// doubled links (run_b0a85008: 4 pasted, 2 links, both the same URL), so
// each picture is pasted alone and counts only once a NEW remote link for
// it is in the text and the page's uploads are quiet. Every picture must
// end with its own distinct link; anything less stops before the submit.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	mdImageLinkRe = regexp.MustCompile(`!\[[^\]\n]*\]\((https?://[^)\s]+)`)
	mdImageTagRe  = regexp.MustCompile(`(?i)<img[^>]+src=["'](https?://[^"']+)`)
)

// markdownImageURLs: the remote picture URLs a markdown text links, in
// order, each once.
func markdownImageURLs(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{mdImageLinkRe, mdImageTagRe} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if u := m[1]; !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}

// publishPasteImageFn pastes picker file k; an SVG is drawn to a PNG first
// (CSDN's picture upload takes png/jpg/gif/webp/bmp only).
const publishPasteImageFn = `async function(k, svgToPNG) {
  let f = document.getElementById('__pub_picker').files[k];
  if (svgToPNG && (/svg/i.test(f.type) || /\.svg$/i.test(f.name))) {
    const u = URL.createObjectURL(f);
    const img = new Image();
    await new Promise((res, rej) => { img.onload = res; img.onerror = () => rej(new Error('svg did not load')); img.src = u; });
    const w = img.naturalWidth || 1200, h = img.naturalHeight || 800;
    const s = Math.max(1, Math.min(3, 1600 / w));
    const c = document.createElement('canvas');
    c.width = Math.round(w * s); c.height = Math.round(h * s);
    const g = c.getContext('2d');
    g.fillStyle = '#fff'; g.fillRect(0, 0, c.width, c.height);
    g.drawImage(img, 0, 0, c.width, c.height);
    URL.revokeObjectURL(u);
    const b = await new Promise(r => c.toBlob(r, 'image/png'));
    if (!b) throw new Error('svg could not be converted');
    f = new File([b], f.name.replace(/\.svg$/i, '') + '.png', {type: 'image/png'});
  }
  this.focus();
  const sel = getSelection(); const r = document.createRange(); r.selectNodeContents(this); r.collapse(false); sel.removeAllRanges(); sel.addRange(r);
  const dt = new DataTransfer(); dt.items.add(f);
  this.dispatchEvent(new ClipboardEvent('paste', {clipboardData: dt, bubbles: true, cancelable: true}));
  return f.type || 'file';
}`

// mdPictureWait: one picture's upload and link (CSDN takes 5-15 s).
const mdPictureWait = 30 * time.Second

func (p *publishRun) mdText(obj string) string {
	v, _ := p.call(obj, readValueFn)
	return v
}

// quietUploads waits for the page's writes (uploads, autosave) to settle.
func (p *publishRun) quietUploads(max time.Duration) {
	if p.net != nil {
		p.net.waitQuiet(800*time.Millisecond, max)
	}
}

// errPictureLost: inserting one picture removed a picture link the editor
// held before. It is measured, not guessed: the earlier link is gone from
// the editor text while the new upload was stored or not.
type errPictureLost struct {
	lost, at int
	stored   bool
}

func (e errPictureLost) Error() string {
	how := "and no new link for it appeared"
	if e.stored {
		how = "although the site stored its upload"
	}
	return fmt.Sprintf("regression: inserting picture %d removed picture %d's link from the editor text (%s); nothing was submitted", e.at, e.lost, how)
}

// prepInsert runs the strategy's native key chords in the editor before a
// picture goes in (trusted input: editors that keep their own caret update
// it from real keys, not from a script-set selection).
func (p *publishRun) prepInsert(obj string) error {
	if p.spec.InsertSettle > 0 {
		time.Sleep(p.spec.InsertSettle)
	}
	if len(p.spec.InsertPrepKeys) == 0 {
		return nil
	}
	if _, err := p.call(obj, "function(){ this.focus(); return 'ok'; }"); err != nil {
		return err
	}
	for _, chord := range p.spec.InsertPrepKeys {
		if err := p.pressChord(chord); err != nil {
			return fmt.Errorf("key %s: %v", chord, err)
		}
		time.Sleep(60 * time.Millisecond)
	}
	return nil
}

// pasteOneMarkdown pastes picture k and waits for a link not in known; an
// earlier link (have) that disappears meanwhile is a regression.
func (p *publishRun) pasteOneMarkdown(obj string, k int, known map[string]bool, have []string) (string, error) {
	if err := p.prepInsert(obj); err != nil {
		return "", err
	}
	t0 := time.Now()
	if _, err := p.send("Runtime.callFunctionOn", map[string]interface{}{"objectId": obj, "functionDeclaration": publishPasteImageFn, "arguments": []map[string]interface{}{{"value": k}, {"value": true}}, "awaitPromise": true, "returnByValue": true}); err != nil {
		return "", err
	}
	end := time.Now().Add(mdPictureWait)
	for time.Now().Before(end) {
		time.Sleep(500 * time.Millisecond)
		ok, _, failed, _ := p.net.uploadStats(t0)
		if failed > 0 {
			return "", fmt.Errorf("the site's upload failed: %s", strings.Join(p.net.failedSince(t0), "; "))
		}
		text := p.mdText(obj)
		for i, u := range have {
			if !strings.Contains(text, u) {
				// Give the editor a moment: a re-render can drop and
				// restore text; a lost link stays lost.
				time.Sleep(1500 * time.Millisecond)
				if !strings.Contains(p.mdText(obj), u) {
					return "", errPictureLost{lost: i + 1, at: k + 1, stored: ok > 0}
				}
			}
		}
		var fresh []string
		for _, u := range markdownImageURLs(text) {
			if !known[u] {
				fresh = append(fresh, u)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		// The link is in; let the upload and the editor's save finish
		// before the next paste, then take what the text holds now.
		p.quietUploads(15 * time.Second)
		for _, u := range markdownImageURLs(p.mdText(obj)) {
			if !known[u] {
				known[u] = true
			}
		}
		return fresh[0], nil
	}
	return "", fmt.Errorf("no new picture link after %s", mdPictureWait)
}

// pasteImagesMarkdown puts every picture file into the markdown editor, one
// at a time, and checks each has its own remote link and that no earlier
// one was lost on the way. Facts: pictures_wanted, pictures_kept, and on a
// loss regression=1 lost_item=N.
func (p *publishRun) pasteImagesMarkdown(obj string) (string, error) {
	if _, err := p.loadPicker(); err != nil {
		return "", err
	}
	files := len(p.spec.Files)
	if p.spec.TrialPictures > 0 && p.spec.TrialPictures < files {
		files = p.spec.TrialPictures
	}
	p.rep.fact("pictures_wanted", float64(files))
	p.rep.fact("regression", 0)
	known := map[string]bool{}
	for _, u := range markdownImageURLs(p.mdText(obj)) {
		known[u] = true
	}
	start := len(known)
	var links []string
	var notes []string
	kept := func() {
		text := p.mdText(obj)
		n := 0
		for _, u := range links {
			if strings.Contains(text, u) {
				n++
			}
		}
		p.rep.fact("pictures_kept", float64(n))
	}
	for k := 0; k < files; k++ {
		p.quietUploads(15 * time.Second)
		u, err := p.pasteOneMarkdown(obj, k, known, links)
		var lost errPictureLost
		if err != nil && !errors.As(err, &lost) && !strings.Contains(err.Error(), "upload failed") {
			// One more try; a late link from the first paste counts too.
			notes = append(notes, fmt.Sprintf("picture %d retried (%v)", k+1, err))
			p.quietUploads(10 * time.Second)
			u, err = p.pasteOneMarkdown(obj, k, known, links)
		}
		if errors.As(err, &lost) {
			kept()
			p.rep.fact("regression", 1)
			p.rep.fact("lost_item", float64(lost.lost))
			return strings.Join(notes, "; "), err
		}
		if err != nil {
			kept()
			return strings.Join(notes, "; "), fmt.Errorf("picture %d of %d was not stored by the site (%v); %d of %d picture(s) are in the editor; nothing was submitted", k+1, files, err, len(links), files)
		}
		links = append(links, u)
	}
	text := p.mdText(obj)
	have := 0
	for _, u := range links {
		if strings.Contains(text, u) {
			have++
		}
	}
	p.rep.fact("pictures_kept", float64(have))
	if have < files {
		p.rep.fact("regression", 1)
		return "", fmt.Errorf("after pasting, the editor keeps %d of the %d picture link(s); nothing was submitted", have, files)
	}
	p.mdLinks = links
	out := fmt.Sprintf("pasted %d picture(s) one at a time; each has its own stored link (links %d->%d)", len(links), start, len(markdownImageURLs(text)))
	if len(p.spec.InsertPrepKeys) > 0 {
		out += "; before each picture pressed " + strings.Join(p.spec.InsertPrepKeys, ",")
	}
	if len(notes) > 0 {
		out += "; " + strings.Join(notes, "; ")
	}
	return out, nil
}

// checkMarkdownLinks: the pictures' links are all still in the text.
func (p *publishRun) checkMarkdownLinks(text string) error {
	for i, u := range p.mdLinks {
		if !strings.Contains(text, u) {
			return fmt.Errorf("picture %d's link is no longer in the body", i+1)
		}
	}
	return nil
}

// bodyComplete: the editor holds (nearly) the whole text, not just its
// start and end (a CSDN body once kept 23 characters).
func bodyComplete(got, want string) error {
	g, w := len([]rune(normText(got))), len([]rune(normText(want)))
	if w > 0 && g*10 < w*9 {
		return fmt.Errorf("the editor holds %d of the %d characters of the body", g, w)
	}
	return nil
}
