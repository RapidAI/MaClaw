// Keep production HTML compatible with Wails' index.html injector
// (/wails/runtime.js + /wails/ipc.js). Rewriting stylesheets to media="not all"
// made the splash paint, but window.go.main.App never bound and React unmounted
// to a blank #root. Only strip crossorigin so classic runtime scripts still run.
export function rewriteWebviewFirstPaintHtml(html: string): string {
  return html.replace(/\s+crossorigin(?:="[^"]*")?/gi, '')
}
