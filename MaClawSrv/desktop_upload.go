package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// desktopUploadHookJS makes file inputs that a site creates on click (often
// detached, or inside a shadow root) reachable and stops them from opening
// the OS file chooser: input.click()/showPicker() on a file input marks it,
// attaches a detached one to <body> hidden, and returns without a dialog.
// DOM.setFileInputFiles then sets the files and fires change/input, which is
// what every site's upload handler listens for.
const desktopUploadHookJS = `(() => {
  if (!window.__maclawUploadHook) {
    window.__maclawUploadHook = true;
    const mark = (el) => {
      el.setAttribute('data-maclaw-upload', '1');
      if (!el.isConnected) { el.style.display = 'none'; document.body.appendChild(el); }
    };
    const origClick = HTMLInputElement.prototype.click;
    HTMLInputElement.prototype.click = function () {
      if ((this.type || '').toLowerCase() === 'file') { mark(this); return; }
      return origClick.apply(this, arguments);
    };
    if (HTMLInputElement.prototype.showPicker) {
      const origPick = HTMLInputElement.prototype.showPicker;
      HTMLInputElement.prototype.showPicker = function () {
        if ((this.type || '').toLowerCase() === 'file') { mark(this); return; }
        return origPick.apply(this, arguments);
      };
    }
  }
  const all = [];
  const walk = (root) => {
    root.querySelectorAll('input[type=file]').forEach(e => all.push(e));
    root.querySelectorAll('*').forEach(n => { if (n.shadowRoot) walk(n.shadowRoot); });
  };
  walk(document);
  return 'file inputs: ' + all.length;
})()`

// desktopUploadSelectors: the input a click just created first, then any
// file input (shadow roots and frames are searched by the browser layer).
var desktopUploadSelectors = []string{"input[type=file][data-maclaw-upload]", "input[type=file]"}

func desktopUploadFiles(args map[string]any) []string {
	var files []string
	add := func(v string) {
		for _, f := range strings.Split(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				files = append(files, f)
			}
		}
	}
	switch v := args["files"].(type) {
	case []any:
		for _, x := range v {
			add(fmt.Sprint(x))
		}
	case string:
		add(v)
	}
	add(desktopArg(args, "path"))
	return files
}

// desktopUpload puts local files into a page's file input over CDP
// (DOM.setFileInputFiles) without the OS file chooser. Optional
// click/ref names the page's upload button for sites that create the input
// only on click; the hook keeps that click from opening a dialog.
func desktopUpload(session *browser.BrowserAgentSession, args map[string]any) (string, error) {
	files := desktopUploadFiles(args)
	if len(files) == 0 {
		return "", fmt.Errorf("upload needs files (absolute paths on the desktop)")
	}
	session.Policy.AllowUpload = true
	// The hook is our own code, not a model script: evaluate it directly.
	// Through the page-script guard it was refused (".prototype"), so every
	// upload failed as "page not ready" (run_65ce783c). Without the hook
	// the run-wide file-chooser interception still covers clicks.
	_, _ = session.EvalValue(desktopUploadHookJS)
	before := desktopUploadState(session)
	selectors := desktopUploadSelectors
	if sel := desktopArg(args, "selector"); sel != "" {
		selectors = append([]string{sel}, selectors...)
	}
	try := func() (*browser.BrowserActionResult, error) {
		var last error
		for _, sel := range selectors {
			r, err := session.SetFilesOn("", "", sel, files)
			if err == nil {
				return r, nil
			}
			last = err
		}
		return nil, last
	}
	clicked := ""
	var r *browser.BrowserActionResult
	var err error
	if desktopArg(args, "ref") == "" && desktopArg(args, "text") == "" {
		r, err = try()
	} else {
		armed := session.ArmFileChooser(files) == nil
		if armed {
			defer session.DisarmFileChooser()
		}
		if ref := desktopArg(args, "ref"); ref != "" {
			_, err = session.Click(desktopArg(args, "snapshot_id"), ref, "")
			clicked = ref
		} else {
			_, err = session.ClickText(desktopArg(args, "snapshot_id"), desktopArg(args, "text"))
			clicked = desktopArg(args, "text")
		}
		if err == nil && armed {
			if ok, chErr := session.WaitFileChooser(3 * time.Second); ok && chErr == nil {
				if ok, why := desktopUploadVerify(session, before); ok {
					return fmt.Sprintf("uploaded %d file(s): the page's file chooser was answered over CDP (no OS dialog) after clicking %s; page: %s", len(files), clicked, why) + desktopPublishNext, nil
				}
			}
		}
		if err == nil {
			r, err = try()
		}
	}
	if err != nil {
		return "", fmt.Errorf("upload: no file input took the files (%v). Pass text= (or ref=) of the page's upload button so the input it creates is caught; do not open the OS file chooser", err)
	}
	if ok, why := desktopUploadVerify(session, before); !ok {
		return "", fmt.Errorf("upload: the files were set on a file input but the page shows no change (%s). Check you are on the page's upload area (the right tab/mode), then pass text= of its upload button; do not open the OS file chooser", why)
	} else {
		before = why
	}
	msg := fmt.Sprintf("uploaded %d file(s) via the page's file input (no OS dialog); page: %s", len(files), before)
	if clicked != "" {
		msg += " after clicking " + clicked
	}
	_ = r
	return msg + desktopPublishNext, nil
}

// desktopUploadStateJS: what a page shows after an upload. Many sites clear
// input.files right after reading them (XHS does), so previews (blob:/data:
// images, <video>, canvas), all images and the DOM size count too.
const desktopUploadStateJS = `(() => { let files = 0; const walk = (r) => { r.querySelectorAll('input[type=file]').forEach(e => files += e.files.length); r.querySelectorAll('*').forEach(n => n.shadowRoot && walk(n.shadowRoot)); }; walk(document);
  const imgs = [...document.images]; return [files, imgs.filter(i => /^(blob|data):/.test(i.src)).length, imgs.length, document.querySelectorAll('video,canvas').length, document.getElementsByTagName('*').length].join(','); })()`

func desktopUploadState(session *browser.BrowserAgentSession) string {
	v, err := session.EvalValue(desktopUploadStateJS)
	if err != nil {
		return ""
	}
	return strings.Trim(v, `"`)
}

func desktopUploadParse(v string) []int {
	out := make([]int, 5)
	for i, f := range strings.Split(v, ",") {
		if i < 5 {
			fmt.Sscan(f, &out[i])
		}
	}
	return out
}

// desktopUploadJudge reports whether after differs from before the way an
// accepted upload does.
func desktopUploadJudge(before, after string) (bool, string) {
	b, a := desktopUploadParse(before), desktopUploadParse(after)
	why := fmt.Sprintf("input files %d, previews %d->%d, images %d->%d, media %d->%d, nodes %d->%d", a[0], b[1], a[1], b[2], a[2], b[3], a[3], b[4], a[4])
	if before == "" && after == "" {
		return true, "state unreadable"
	}
	ok := a[0] > 0 || a[1] > b[1] || a[2] > b[2] || a[3] > b[3] || a[4] > b[4]+20
	return ok, why
}

func desktopUploadVerify(session *browser.BrowserAgentSession, before string) (bool, string) {
	var why string
	for i := 0; i < 8; i++ {
		time.Sleep(500 * time.Millisecond)
		ok, w := desktopUploadJudge(before, desktopUploadState(session))
		if ok {
			return true, w
		}
		why = w
	}
	return false, why
}

// desktopPublishNext steers the rest of a post into the one checked call.
const desktopPublishNext = "\nNEXT: finish the post with ONE action=publish (title=, body= the full text with any #tags you chose yourself, submit_texts=[the publish button text]); files are already in, so leave files empty. It fills, reads back, clicks publish and verifies. Do not fill fields or add the page's suggested tags by script."
