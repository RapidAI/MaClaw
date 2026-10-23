package main

import (
	"fmt"
	"strings"

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
	hook, hookErr := browser.RunPageScriptBatch(session, "return "+desktopUploadHookJS, "", "", "")
	_ = hook
	if hookErr != nil {
		return "", fmt.Errorf("upload: page not ready: %w", hookErr)
	}
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
		if ref := desktopArg(args, "ref"); ref != "" {
			_, err = session.Click(desktopArg(args, "snapshot_id"), ref, "")
			clicked = ref
		} else {
			_, err = session.ClickText(desktopArg(args, "snapshot_id"), desktopArg(args, "text"))
			clicked = desktopArg(args, "text")
		}
		if err == nil {
			r, err = try()
		}
	}
	if err != nil {
		return "", fmt.Errorf("upload: no file input took the files (%v). Pass text= (or ref=) of the page's upload button so the input it creates is caught; do not open the OS file chooser", err)
	}
	msg := fmt.Sprintf("uploaded %d file(s) via the page's file input (no OS dialog)", len(files))
	if clicked != "" {
		msg += " after clicking " + clicked
	}
	_ = r
	return msg, nil
}
