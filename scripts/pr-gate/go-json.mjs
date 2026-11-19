// Decide whether `go test -json` output is a real pass.
// A package that prints "[no tests to run]" is still Action "pass".

export function packageMatches(jsonPackage, dir) {
  const slash = dir.replace(/^\.\//, "");
  return jsonPackage === slash || jsonPackage.endsWith(`/${slash}`);
}

export function evaluateGoJSON(text, expectedByPackage) {
  const problems = [];
  const events = [];
  const lines = String(text).split("\n");
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      events.push(JSON.parse(trimmed));
    } catch {
      problems.push(`non-JSON test output: ${trimmed.slice(0, 160)}`);
    }
  }
  const packages = new Map();
  for (const event of events) {
    const key = event.Package || "";
    if (!packages.has(key)) {
      packages.set(key, { outputs: [], tests: new Set(), failed: false, passedPackage: false });
    }
    const record = packages.get(key);
    if (event.Action === "output" && event.Output) record.outputs.push(event.Output);
    if (event.Action === "run" && event.Test && !String(event.Test).includes("/")) {
      record.tests.add(event.Test);
    }
    if (event.Action === "fail") record.failed = true;
    if (event.Action === "pass" && !event.Test) record.passedPackage = true;
  }

  const entries = Object.entries(expectedByPackage || {});
  if (entries.length === 0) {
    problems.push("no packages were required");
    return { ok: false, problems, packages };
  }

  for (const [dir, names] of entries) {
    const found = [...packages.entries()].find(([pkg]) => packageMatches(pkg, dir));
    if (!found) {
      problems.push(`no result for ${dir}`);
      continue;
    }
    const [pkg, info] = found;
    const blob = info.outputs.join("");
    if (blob.includes("[no tests to run]") || blob.includes("[no test files]")) {
      problems.push(`${pkg} ran zero tests`);
    }
    if (info.failed || !info.passedPackage) problems.push(`${pkg} did not pass`);
    if (!Array.isArray(names)) {
      if (info.tests.size < 1) problems.push(`${pkg} ran zero tests`);
      continue;
    }
    for (const name of names) {
      if (!info.tests.has(name)) problems.push(`${pkg} did not run ${name}`);
    }
  }
  return { ok: problems.length === 0, problems, packages };
}
