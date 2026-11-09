const MENU_ID = "add-to-companion";
const MENU_LINK_ID = "add-to-companion-link";
const HOST = "com.maclaw.companion";

// Chrome matches documentUrlPatterns against the frame URL and ANDs them with
// targetUrlPatterns. One item cannot mean "this frame is a paper OR this link
// is a paper": a target filter hides the page click, and a document filter of
// every http page shows the command on every site.
// The page item covers a PDF viewer and a paper detail page, including a
// right-click on a link that is not itself a paper (author, license, DOI).
// The link item covers a PDF or paper link on any ordinary page. Both match
// only when that link is itself a paper or PDF and the page already is one
// (arXiv "View PDF"): Chrome then groups the two titles. Same action either
// way. Do not add the frame context. An embedded PDF still matches the page
// item because the frame URL is the PDF.
const PDF_PATTERNS = ["*://*/*.pdf", "*://*/*.PDF", "file:///*.pdf", "file:///*.PDF"];

function hostPaths(hosts, paths) {
  const out = [];
  for (const host of hosts) {
    for (const path of paths) out.push("*://" + host + path);
  }
  return out;
}

function aclOldPatterns() {
  const out = [];
  const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
  for (const host of ["aclanthology.org", "www.aclanthology.org"]) {
    for (const ch of letters) out.push("*://" + host + "/" + ch + "*-*");
  }
  return out;
}

const PAPER_PAGE_PATTERNS = hostPaths(
  ["arxiv.org", "*.arxiv.org", "ar5iv.labs.arxiv.org"],
  ["/abs/*", "/pdf/*", "/html/*", "/src/*"],
).concat(hostPaths(
  ["openreview.net", "www.openreview.net"],
  ["/forum", "/forum/*", "/pdf", "/pdf/*"],
)).concat(hostPaths(
  ["biorxiv.org", "www.biorxiv.org", "medrxiv.org", "www.medrxiv.org"],
  ["/content/*"],
)).concat(hostPaths(
  ["aclanthology.org", "www.aclanthology.org"],
  ["/*.*"],
)).concat(aclOldPatterns()).concat(hostPaths(
  ["ncbi.nlm.nih.gov", "www.ncbi.nlm.nih.gov"],
  ["/pmc/articles/*"],
));

const PAPER_DOCUMENT_PATTERNS = PDF_PATTERNS.concat(PAPER_PAGE_PATTERNS);

// removeAll is async. A newer installMenu call invalidates the older callback
// so two overlapping wakes cannot delete the menu the other one just created.
let menuEpoch = 0;
function installMenu() {
  const epoch = ++menuEpoch;
  chrome.contextMenus.removeAll(() => {
    if (epoch !== menuEpoch) return;
    void chrome.runtime.lastError;
    const title = chrome.i18n.getMessage("menuAdd");
    chrome.contextMenus.create({
      id: MENU_ID,
      title,
      contexts: ["page", "selection", "link"],
      documentUrlPatterns: PAPER_DOCUMENT_PATTERNS,
    }, () => { void chrome.runtime.lastError; });
    chrome.contextMenus.create({
      id: MENU_LINK_ID,
      title,
      contexts: ["link"],
      documentUrlPatterns: ["http://*/*", "https://*/*", "file:///*"],
      targetUrlPatterns: PAPER_DOCUMENT_PATTERNS,
    }, () => { void chrome.runtime.lastError; });
  });
}

// A service worker is created again on each wake. onInstalled does not run
// then, so the menu has to be created at evaluation time.
installMenu();

function browserName() {
  const ua = navigator.userAgent || "";
  if (ua.includes("Edg/")) return "edge";
  try {
    if (self.location && self.location.protocol === "safari-web-extension:") return "safari";
  } catch (_) {}
  return "chrome";
}

function sendHello() {
  const version = (chrome.runtime.getManifest() || {}).version || "";
  chrome.runtime.sendNativeMessage(HOST, {
    hello: true,
    browser: browserName(),
    version,
  }, () => { void chrome.runtime.lastError; });
}

chrome.runtime.onInstalled.addListener(() => {
  installMenu();
  sendHello();
});
chrome.runtime.onStartup.addListener(() => {
  installMenu();
  sendHello();
});

function isHttp(raw) {
  return /^https?:\/\//i.test(String(raw || "").trim());
}

function isFilePdf(raw) {
  try {
    const url = new URL(String(raw || "").trim());
    return url.protocol === "file:" && url.pathname.toLowerCase().endsWith(".pdf");
  } catch (_) {
    return false;
  }
}

function pathOf(raw) {
  try {
    return new URL(String(raw || "").trim()).pathname.toLowerCase();
  } catch (_) {
    return "";
  }
}

function hostOf(raw) {
  try {
    return new URL(String(raw || "").trim()).hostname.toLowerCase();
  } catch (_) {
    return "";
  }
}

function isArxivPaper(host, path) {
  const arxiv = host === "arxiv.org" || host.endsWith(".arxiv.org") || host === "ar5iv.labs.arxiv.org";
  if (!arxiv) return false;
  return path.startsWith("/abs/") || path.startsWith("/pdf/") || path.startsWith("/html/") || path.startsWith("/src/");
}

// Anthology paper ids look like 2024.acl-long.1 or P19-1478. A site section
// such as /people or /volumes is not a paper.
function isAclPaper(host, path) {
  if (host !== "aclanthology.org" && host !== "www.aclanthology.org") return false;
  const segment = (path.split("/").filter(Boolean)[0] || "").replace(/\.pdf$/i, "");
  if (!segment || !/\d/.test(segment)) return false;
  if (/^[a-z]\d{2}-\d{3,}$/i.test(segment)) return true;
  return segment.includes(".") && /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/i.test(segment);
}

// A PDF open in the viewer, a PDF link, or a paper detail page.
function isPaperTarget(raw) {
  const value = String(raw || "").trim();
  if (!value) return false;
  if (isFilePdf(value)) return true;
  if (!isHttp(value)) return false;
  const path = pathOf(value);
  if (path.endsWith(".pdf")) return true;
  const host = hostOf(value);
  if (isArxivPaper(host, path)) return true;
  if ((host === "openreview.net" || host === "www.openreview.net") && (path === "/forum" || path.startsWith("/forum/") || path === "/pdf" || path.startsWith("/pdf/"))) return true;
  if ((host === "biorxiv.org" || host === "www.biorxiv.org" || host === "medrxiv.org" || host === "www.medrxiv.org") && path.includes("/content/")) return true;
  if (isAclPaper(host, path)) return true;
  if ((host === "ncbi.nlm.nih.gov" || host === "www.ncbi.nlm.nih.gov") && path.includes("/pmc/articles/")) return true;
  return false;
}

function chooseURL(linkURL, selection, frameURL, pageURL) {
  const link = String(linkURL || "").trim();
  const selected = String(selection || "").trim();
  const frame = String(frameURL || "").trim();
  const page = String(pageURL || "").trim();
  if (isPaperTarget(link)) return link;
  if (isPaperTarget(selected)) return selected;
  // A PDF viewer embedded in the page reports the PDF as frameUrl.
  if (isPaperTarget(frame) && frame !== page) return frame;
  if (isPaperTarget(page)) return page;
  if (isHttp(link)) return link;
  if (isHttp(selected)) return selected;
  return page;
}

function showNativeResult(ok) {
  if (!chrome.action) return;
  if (!ok) chrome.action.setBadgeBackgroundColor({ color: "#c62828" });
  chrome.action.setBadgeText({ text: ok ? "" : "!" });
  chrome.action.setTitle({ title: chrome.i18n.getMessage(ok ? "menuAdd" : "nativeFailed") });
}

chrome.contextMenus.onClicked.addListener((info, tab) => {
  if (!info || (info.menuItemId !== MENU_ID && info.menuItemId !== MENU_LINK_ID)) return;
  const link = String(info.linkUrl || "").trim();
  const selected = String(info.selectionText || "").trim();
  const frame = String(info.frameUrl || "").trim();
  const page = String(info.pageUrl || (tab && tab.url) || "").trim();
  const url = chooseURL(link, selected, frame, page);
  if (!url) return;
  let title = (tab && tab.title) || "";
  if (url === selected) title = selected;
  else if (url === link) title = link;
  title = title.replace(/[\u0000-\u001f]/g, "");
  if ([...title].length > 200) title = [...title].slice(0, 200).join("");
  chrome.runtime.sendNativeMessage(HOST, { url, title }, (response) => {
    const failed = !!chrome.runtime.lastError || !response || response.ok !== true;
    showNativeResult(!failed);
  });
});
