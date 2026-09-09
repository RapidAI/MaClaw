/** Headings the coding agent must not show in the user-visible answer. */
const CODING_AGENT_AUDIT_HEADINGS = [
    "## \u8d28\u91cf\u5ba1\u8ba1",
    "## \u9a8c\u8bc1\u72b6\u6001",
    "## \u63a2\u7d22\u72b6\u6001",
    "## Diff \u81ea\u68c0",
    "## \u6587\u4ef6\u53d8\u66f4",
    "## \u5b89\u5168\u8fb9\u754c",
    "## \u547d\u4ee4\u9a8c\u8bc1",
    "## \u52a8\u6001\u5de5\u5177",
    "## \u6b65\u9aa4\u6e05\u5355",
    "## \u8fdc\u7a0b Diff \u81ea\u68c0",
    "## \u786e\u8ba4\u72b6\u6001",
    "## \u547d\u4ee4\u72b6\u6001",
    "## \u65e0\u6539\u52a8\u8bc1\u636e",
    "## \u9a8c\u8bc1\u7ed3\u679c",
    "## \u6d89\u53ca\u6587\u4ef6",
    "## \u9a8c\u8bc1\u547d\u4ee4",
    "**\u9a8c\u8bc1\u7ed3\u679c**",
    "**\u6d89\u53ca\u6587\u4ef6**",
    "## Execution report",
    "## \u6267\u884c\u62a5\u544a",
    "## \u57f7\u884c\u5831\u544a",
    "## Coding Task Execution Report",
    "## Coding Execution Report",
    "## Execution Stats",
    "### Failed Tasks",
    "### \u8ba1\u5212\u6267\u884c\u7ed3\u679c",
    "## \u6309\u5df2\u6279\u51c6\u8ba1\u5212\u6267\u884c",
    "\u6267\u884c\u6b65\u9aa4\uff1a",
    "## Involved files",
    "## Files involved",
];

const GENERIC_AUDIT_HEADINGS = ["## Summary", "## \u6458\u8981", "## Verification"];

const PLAN_STEP_HEADING = /(?:^|\n)### T\d+/;

const BOARD_PROGRESS =
    /\u5168\u529f\u80fd\u7f16\u7a0b|\u5168\u529f\u80fd\u8fdc\u7a0b|\u4ed3\u5e93\u5206\u6790\uff1a\u6b63\u5728|\u6267\u884c\u5df2\u6279\u51c6\u8ba1\u5212|\u6267\u884c\u6b65\u9aa4[\uff1a:]|\u8ba1\u5212\u6267\u884c\u7ed3\u679c|\u8fdc\u7a0b\u9879\u76ee\u76ee\u5f55|SSH \u4f1a\u8bdd[\uff1a:]|\u6b65\u7ea7\u9a8c\u8bc1|\u6309\u5df2\u6279\u51c6\u8ba1\u5212|Local coding execution|Remote task \d|Workflow (?:local|remote) execution|Stats:\s*passed|\u7edf\u8ba1\uff1a\u901a\u8fc7|^[\u2460\u2461\u2462\u2463\u2464\u2705\u26a0\u23f9]|^(?:T\d+\/\d+:|[\u2611\u2610\u2717\u2013] T\d+)/;

export function isCodingAgentPlanApprovalText(text: string): boolean {
    const next = text || "";
    return next.includes("## \u9700\u8981\u786e\u8ba4\u6267\u884c\u8ba1\u5212") || next.includes("## \u9700\u6c42\u7406\u89e3") || next.includes("/plan approve");
}

/**
 * Drop leftover coding-agent audit / plan-board sections from streamed or
 * stored assistant text. Generic "## Summary" is only cut when another
 * coding-audit heading is already present. Plan-approval cards keep their
 * ### T steps.
 */
export function stripCodingAgentAuditSections(text: string): string {
    let next = (text || "").trimEnd();
    if (!next) return text || "";
    if (isCodingAgentPlanApprovalText(next)) return next;
    const hasSpecific = CODING_AGENT_AUDIT_HEADINGS.some((heading) => next.includes(heading));
    const headings = hasSpecific ? [...CODING_AGENT_AUDIT_HEADINGS, ...GENERIC_AUDIT_HEADINGS] : CODING_AGENT_AUDIT_HEADINGS;
    for (const heading of headings) {
        const idx = next.indexOf(heading);
        if (idx >= 0) {
            next = next.slice(0, idx).trimEnd();
        }
    }
    const step = next.search(PLAN_STEP_HEADING);
    if (step >= 0) {
        next = next.slice(0, step).trimEnd();
    }
    return next;
}

const CODING_WORKBENCH_STATUS_MILESTONE =
    /^(?:Task received|Preparing the execution|Execution environment is ready|Building the request|Step complete|Received, (?:now )?processing|\u6536\u5230\uff0c\u6b63\u5728\u5904\u7406|\u5df2\u63a5\u6536\u4efb\u52a1|\u6b63\u5728\u51c6\u5907\u6267\u884c\u8def\u5f84|\u4f1a\u8bdd\u9884\u68c0\u5b8c\u6210|\u4f1a\u8bdd\u5df2\u5c31\u7eea|\u4e0a\u4e0b\u6587\u5df2\u51c6\u5907|\u6b63\u5728\u6574\u7406\u4e0a\u4e0b\u6587|\u6a21\u578b\u8bf7\u6c42\u5df2\u53d1\u9001|\u5df2\u5339\u914d\u76f4\u63a5\u6267\u884c|\u5df2\u9009\u62e9\u5feb\u901f\u6267\u884c|\u5df2\u9009\u62e9\u5b8c\u6574\u6267\u884c|\u6b63\u5728\u542f\u52a8\u4efb\u52a1)/i;

function isCodingWorkbenchStatusLine(line: string): boolean {
    const trimmed = (line || "").trim();
    if (!trimmed) return false;
    let body = trimmed;
    if (body.startsWith("\u2022 ")) body = body.slice(2).trim();
    else if (body.startsWith("[Status]")) body = body.replace(/^\[Status\]\s*/, "").trim();
    return CODING_WORKBENCH_STATUS_MILESTONE.test(body);
}

/** True when reasoning still contains chat [Status] milestones. */
export function reasoningHasCodingStatusMilestone(reasoning: string): boolean {
    return (reasoning || "").split(/\r?\n/).some((line) => isCodingWorkbenchStatusLine(line));
}

/** Drop chat [Status] bullets so the coding trail is not a thinking pane. */
export function stripCodingWorkbenchStatusReasoning(reasoning: string): string {
    const kept = (reasoning || "").split(/\r?\n/).filter((line) => !isCodingWorkbenchStatusLine(line));
    return kept.join("\n").replace(/^\n+|\n+$/g, "");
}

/** Workbench / workflow board banners that are not Coding Agent Event lines. */
export function isCodingAgentBoardProgressContent(content: string): boolean {
    const trimmed = (content || "").trim();
    if (!trimmed) return false;
    if (trimmed.startsWith("Coding Agent Event:") || /^Coding Agent:\s*[a-z]+(?:\s+T\d+)?(?:\s+-|\s*$)/i.test(trimmed)) {
        return false;
    }
    return BOARD_PROGRESS.test(trimmed);
}
