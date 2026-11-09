import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createContext, runInContext } from "node:vm";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const created = [];
const badgeTexts = [];
const actionTitles = [];
let clickHandler;
let nativeResponse = { ok: true };
let nativeError = null;
const chrome = {
  i18n: { getMessage(key) { return key === "nativeFailed" ? "无法连接伴读" : "添加到伴读"; } },
  action: {
    setBadgeBackgroundColor() {},
    setBadgeText({ text }) { badgeTexts.push(text); },
    setTitle({ title }) { actionTitles.push(title); },
  },
  contextMenus: {
    removeAll(cb) { cb(); },
    create(props, cb) { created.push(props); if (cb) cb(); },
    onClicked: { addListener(fn) { clickHandler = fn; } },
  },
  runtime: {
    lastError: null,
    getManifest() { return { version: "1.0.0" }; },
    onInstalled: { addListener() {} },
    onStartup: { addListener() {} },
    sendNativeMessage(_host, _message, cb) {
      chrome.runtime.lastError = nativeError;
      cb(nativeResponse);
      chrome.runtime.lastError = null;
    },
  },
};
const sandbox = {
  chrome,
  navigator: { userAgent: "Mozilla/5.0 Chrome/120.0" },
  self: { location: { protocol: "chrome-extension:" } },
  URL,
  console,
};
runInContext(readFileSync(join(dirname(fileURLToPath(import.meta.url)), "background.js"), "utf8"), createContext(sandbox));

assert.equal(created.length, 2);
const pageItem = created.find((item) => item.id === "add-to-companion");
const linkItem = created.find((item) => item.id === "add-to-companion-link");
assert.ok(pageItem && linkItem);
const list = (value) => Array.from(value).join(",");
assert.equal(list(pageItem.contexts), "page,selection,link");
assert.equal(pageItem.targetUrlPatterns, undefined);
assert.equal(list(linkItem.contexts), "link");
assert.equal(pageItem.title, "添加到伴读");
assert.equal(linkItem.title, pageItem.title);
assert.equal(list(linkItem.documentUrlPatterns), "http://*/*,https://*/*,file:///*");
assert.equal(Array.from(pageItem.documentUrlPatterns).includes("http://*/*"), false);
assert.equal(Array.from(pageItem.documentUrlPatterns).includes("https://*/*"), false);
assert.equal(Array.from(pageItem.documentUrlPatterns).includes("file://*/*"), false);
assert.equal(list(linkItem.targetUrlPatterns), list(pageItem.documentUrlPatterns));

function matchPattern(pattern, raw) {
  let url;
  try { url = new URL(raw); } catch { return false; }
  const schemeEnd = pattern.indexOf("://");
  if (schemeEnd < 0) return false;
  const scheme = pattern.slice(0, schemeEnd);
  const rest = pattern.slice(schemeEnd + 3);
  const slash = rest.indexOf("/");
  if (slash < 0) return false;
  const host = rest.slice(0, slash);
  const path = rest.slice(slash);
  const uscheme = url.protocol.replace(":", "");
  if (scheme === "*") {
    if (uscheme !== "http" && uscheme !== "https") return false;
  } else if (scheme.toLowerCase() !== uscheme) return false;
  const uhost = url.hostname.toLowerCase();
  if (uscheme === "file") {
    if (host !== "") return false;
  } else if (host === "*") {
    // any http(s) host
  } else if (host.startsWith("*.")) {
    const base = host.slice(2).toLowerCase();
    if (!uhost.endsWith("." + base)) return false;
    const prefix = uhost.slice(0, uhost.length - base.length - 1);
    if (!prefix || prefix.includes(".")) return false;
  } else if (uhost !== host.toLowerCase()) return false;
  const body = path.split("*").map((part) => part.replace(/[.+?^${}()|[\]\\]/g, "\\$&")).join(".*");
  return new RegExp("^" + body + "$").test(url.pathname);
}

function showsOnPage(raw) {
  return pageItem.documentUrlPatterns.some((pattern) => matchPattern(pattern, raw));
}
function showsOnLink(raw) {
  return linkItem.targetUrlPatterns.some((pattern) => matchPattern(pattern, raw));
}

assert.equal(showsOnPage("https://google.com/search?q=pdf"), false);
assert.equal(showsOnPage("https://example.com/notes.html"), false);
assert.equal(showsOnLink("https://example.com/notes.html"), false);
assert.equal(showsOnPage("https://example.com/files/paper.pdf"), true);
assert.equal(showsOnPage("https://example.com/files/paper.PDF"), true);
assert.equal(showsOnLink("https://publisher.example.org/a/b/paper.pdf?download=1"), true);
assert.equal(showsOnPage("file:///C:/docs/paper.pdf"), true);
assert.equal(showsOnPage("file:///C:/docs/notes.html"), false);
assert.equal(showsOnPage("https://arxiv.org/abs/2510.16079"), true);
assert.equal(showsOnPage("https://arxiv.org/abs/2611.08483"), true);
assert.equal(showsOnLink("https://arxiv.org/search/advanced?terms-0-operator=AND&terms-0-term=Jiang&terms-0-field=author"), false);
assert.equal(showsOnLink("https://arxiv.org/pdf/2611.08483"), true);
assert.equal(showsOnPage("https://arxiv.org/pdf/2510.16079"), true);
assert.equal(showsOnPage("https://ar5iv.labs.arxiv.org/html/2510.16079"), true);
assert.equal(showsOnLink("https://export.arxiv.org/src/2510.16079"), true);
assert.equal(showsOnPage("https://openreview.net/forum?id=abc"), true);
assert.equal(showsOnPage("https://openreview.net/forums"), false);
assert.equal(showsOnPage("https://www.biorxiv.org/content/10.1101/2020.01.01.123456v1"), true);
assert.equal(showsOnPage("https://www.biorxiv.org/"), false);
assert.equal(showsOnPage("https://aclanthology.org/2024.acl-long.1"), true);
assert.equal(showsOnPage("https://aclanthology.org/P19-1478"), true);
assert.equal(showsOnPage("https://aclanthology.org/people"), false);
assert.equal(showsOnPage("https://aclanthology.org/people/jane-doe"), false);
assert.equal(showsOnPage("https://www.ncbi.nlm.nih.gov/pmc/articles/PMC1234567/"), true);
assert.equal(showsOnPage("https://pmc.ncbi.nlm.nih.gov/articles/PMC1234567/"), false);

assert.equal(
  sandbox.chooseURL("https://example.com/a.pdf", "", "https://example.com/wrap", "https://example.com/wrap"),
  "https://example.com/a.pdf",
);
assert.equal(
  sandbox.chooseURL("", "", "https://example.com/embed.pdf", "https://example.com/wrap"),
  "https://example.com/embed.pdf",
);
assert.equal(
  sandbox.chooseURL("https://arxiv.org/abs/2510.16079", "", "", "https://news.example.org/"),
  "https://arxiv.org/abs/2510.16079",
);

assert.equal(typeof clickHandler, "function");
const clickPaper = () => clickHandler(
  { menuItemId: "add-to-companion", pageUrl: "https://arxiv.org/abs/2510.16079" },
  { title: "A paper" },
);
nativeResponse = { ok: false, code: "save-failed" };
clickPaper();
assert.equal(badgeTexts.at(-1), "!");
assert.equal(actionTitles.at(-1), "无法连接伴读");
nativeResponse = undefined;
nativeError = { message: "native host not found" };
clickPaper();
assert.equal(badgeTexts.at(-1), "!");
assert.equal(actionTitles.at(-1), "无法连接伴读");
nativeResponse = { ok: true };
nativeError = null;
clickPaper();
assert.equal(badgeTexts.at(-1), "");
assert.equal(actionTitles.at(-1), "添加到伴读");

console.log("background menu patterns ok");
