// Read the pull_request path filters from mobilebot.yml.
// A parse failure is fail-closed: this gate will not assume the other
// workflow still covers a mobilebot change.

export function matchGitHubPath(glob, file) {
  const parts = String(glob).split("**");
  let source = "^";
  parts.forEach((part, index) => {
    if (index > 0) source += ".*";
    let buf = "";
    for (const char of part) {
      if (char === "*") buf += "[^/]*";
      else if (char === "?") buf += "[^/]";
      else buf += char.replace(/[.+^${}()|[\]\\]/g, "\\$&");
    }
    source += buf;
  });
  source += "$";
  return new RegExp(source).test(file);
}

export function readPullRequestPaths(yaml) {
  const lines = String(yaml).split("\n");
  let inOn = false;
  let inPullRequest = false;
  let inPaths = false;
  let sawPullRequest = false;
  let sawPathsKey = false;
  const paths = [];
  for (const line of lines) {
    if (/^on:\s*$/.test(line)) {
      inOn = true;
      inPullRequest = false;
      inPaths = false;
      continue;
    }
    if (inOn && /^[^ \t#]/.test(line)) {
      inOn = false;
      inPullRequest = false;
      inPaths = false;
    }
    if (!inOn) continue;
    if (/^  pull_request:\s*$/.test(line)) {
      inPullRequest = true;
      sawPullRequest = true;
      inPaths = false;
      continue;
    }
    if (inPullRequest && /^  [A-Za-z_]/.test(line)) {
      inPullRequest = false;
      inPaths = false;
    }
    if (!inPullRequest) continue;
    if (/^    paths:\s*$/.test(line)) {
      inPaths = true;
      sawPathsKey = true;
      continue;
    }
    if (!inPaths) continue;
    const quoted = line.match(/^      - ["'](.+)["']\s*$/);
    const plain = line.match(/^      - (\S+)\s*$/);
    if (quoted) {
      paths.push(quoted[1]);
      continue;
    }
    if (plain) {
      paths.push(plain[1]);
      continue;
    }
    if (/^\s*$/.test(line) || /^\s*#/.test(line)) continue;
    inPaths = false;
  }
  if (!sawPullRequest) {
    return { ok: false, error: "mobilebot workflow has no pull_request trigger" };
  }
  if (sawPathsKey && paths.length === 0) {
    return { ok: false, error: "mobilebot pull_request paths list is empty" };
  }
  return { ok: true, paths: sawPathsKey ? paths : null };
}

export function workflowCovers(spec, file) {
  if (!spec || !spec.ok) return false;
  if (spec.paths == null) return true;
  return spec.paths.some((glob) => matchGitHubPath(glob, file));
}
