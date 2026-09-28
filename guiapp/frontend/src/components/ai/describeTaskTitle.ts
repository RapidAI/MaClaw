/**
 * Turn a raw first instruction into a short task title.
 * Already-short names pass through. Parameter dumps, hosts, and secrets do not.
 */

const CLEAN_TITLE_MAX = 32;
const DESCRIBED_TITLE_MAX = 24;

const FILLER_RE = /^(?:(?:请|帮我|帮忙|麻烦你?|我想|我要|能不能|可不可以|please|kindly|can you|could you)\s*)+/i;
const TOKEN_CLASS = "[^\\s,，。；;：:]";
const TOKEN = `${TOKEN_CLASS}+`;
// 密码/口令 are labels, so the next token is the value.
// English words like "token refresh" are topics unless a value is assigned or looks like a secret.
const CN_SECRET_RE = new RegExp(`(?:密码|口令)\\s*[:：=]?\\s*${TOKEN}`, "i");
const EN_SECRET_LABEL = "(?<![A-Za-z0-9])(?:password|passwd|pwd|token|secret|api[_-]?key)(?![A-Za-z0-9])";
const EN_SECRET_RE = new RegExp(`${EN_SECRET_LABEL}(?:\\s*[:：=]\\s*${TOKEN}|\\s+(?=${TOKEN_CLASS}*\\d)${TOKEN})`, "i");
const USER_LABEL_RE = new RegExp(`(?:用户名|用户|username)(?=\\s*[:：=]|\\s+[A-Za-z0-9])\\s*[:：=]?\\s*[A-Za-z0-9]${TOKEN_CLASS}*`, "i");
const ACCOUNT_SECRET_RE = new RegExp(`(?:^|[\\s,，])(?:root|admin)\\s+(?=${TOKEN_CLASS}*\\d)${TOKEN}`, "i");
const FILE_EXT_RE = /^(?:pdf|docx?|pptx?|xlsx?|png|jpe?g|gif|svg|webp|md|txt|json|ya?ml|tsx?|jsx?|mjs|cjs|go|py|css|html?|zip|gz|mp[34]|wav|csv|log)$/i;
const hostCandidate = () => /[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)+/g;
const VERB_RE = /保存到[^\s,，。；;]{1,8}|保存|写入|记录|整理|归档|查看|查询|检查|排查|修复|修改|更新|生成|创建|撰写|写|总结|分析|部署|安装|下载|上传|同步|备份|导出|导入|发送|配置|删除|清理|翻译|搜索|统计|比较|对比|优化|审查|审核|save|write|check|fix|review|create|update|deploy|summarize|analyze|search|export|import|连接/i;
const SETUP_VERB_RE = /^(?:连接|配置|登录)$/;

function chars(text: string): string[] {
    return [...text];
}

function clip(text: string, max: number): string {
    const parts = chars(text.trim());
    if (parts.length <= max) return parts.join("");
    return `${parts.slice(0, max).join("")}…`;
}

function looksLikePath(text: string): boolean {
    return (text.includes("\\") || text.includes("/")) && text.length > 24;
}

function isHostToken(token: string): boolean {
    if (/^(?:\d{1,3}\.){3}\d{1,3}$/.test(token)) return true;
    if (!/^(?:[a-z0-9-]+\.)+[a-z]{2,}$/i.test(token)) return false;
    const suffix = token.split(".").pop() || "";
    return !FILE_EXT_RE.test(suffix);
}

function hasHost(text: string): boolean {
    return [...text.matchAll(hostCandidate())].some((match) => isHostToken(match[0]));
}

function stripHosts(text: string): string {
    return text.replace(hostCandidate(), (token) => (isHostToken(token) ? " " : token));
}

function hasNoise(text: string): boolean {
    return CN_SECRET_RE.test(text) || EN_SECRET_RE.test(text) || USER_LABEL_RE.test(text) || ACCOUNT_SECRET_RE.test(text) || hasHost(text);
}

function looksLikeRawCommand(text: string): boolean {
    if (looksLikePath(text)) return false;
    if (chars(text).length > CLEAN_TITLE_MAX) return true;
    if (hasNoise(text)) return true;
    return /[：:]/.test(text) && chars(text).length > 16;
}

function stripOnce(text: string, pattern: RegExp): string {
    let next = text;
    let guard = 0;
    while (pattern.test(next) && guard < 8) {
        next = next.replace(pattern, " ");
        guard += 1;
    }
    return next;
}

function stripNoise(text: string): string {
    const stripped = stripHosts(stripOnce(stripOnce(stripOnce(stripOnce(text, CN_SECRET_RE), EN_SECRET_RE), ACCOUNT_SECRET_RE), USER_LABEL_RE));
    return stripped
        .replace(FILLER_RE, "")
        .replace(/\s+/g, " ")
        .replace(/\s*([，,：:])\s*/g, "$1")
        .replace(/^[，,：:\s]+|[，,：:\s]+$/g, "")
        .trim();
}

function prettifySubject(subject: string): string {
    const spaced = subject
        .replace(/([A-Za-z0-9])([\u4e00-\u9fff])/g, "$1 $2")
        .replace(/([\u4e00-\u9fff])([A-Za-z0-9])/g, "$1 $2")
        .replace(/\s+/g, " ")
        .trim();
    return spaced.replace(/^([A-Za-z][A-Za-z0-9]{1,11})\b/, (token) => {
        if (/^[a-z][a-z0-9]{1,11}$/.test(token) && /\d/.test(token)) return token.toUpperCase();
        return token;
    });
}

function verbHits(text: string): { verb: string; index: number }[] {
    const hits: { verb: string; index: number }[] = [];
    const flags = VERB_RE.flags.includes("g") ? VERB_RE.flags : `${VERB_RE.flags}g`;
    const re = new RegExp(VERB_RE.source, flags);
    let match: RegExpExecArray | null;
    while ((match = re.exec(text)) !== null) {
        const prev = match.index > 0 ? text.charAt(match.index - 1) : "";
        const next = text.charAt(match.index + match[0].length);
        // 编写/填写 keep 写 inside the word. 写成 still counts as the verb.
        const writeInsideCompound = match[0] === "写" && /[编填书改撰抄默]/.test(prev);
        const asciiInsideWord = /^[A-Za-z]+$/.test(match[0]) && (/[A-Za-z]/.test(prev) || /[A-Za-z]/.test(next));
        if (!writeInsideCompound && !asciiInsideWord) hits.push({ verb: match[0], index: match.index });
        if (match[0].length === 0) break;
    }
    return hits;
}

function baObjectBefore(text: string, verbIndex: number): string {
    const before = text.slice(0, verbIndex);
    const match = before.match(/(?:把|将)\s*([^，,。；;把将]*)$/);
    if (!match) return "";
    return match[1].replace(/^(?:一份|一个|一下|一下子)/, "").trim();
}

function fitTail(object: string, budget: number): string {
    const cleaned = object.trim();
    if (!cleaned) return "";
    if (chars(cleaned).length <= budget) return cleaned;
    if (/\s/.test(cleaned)) {
        const parts = cleaned.split(/\s+/);
        let acc = "";
        for (let i = parts.length - 1; i >= 0; i--) {
            const next = acc ? `${parts[i]} ${acc}` : parts[i];
            if (chars(next).length > budget) break;
            acc = next;
        }
        if (acc) return acc;
    }
    return chars(cleaned).slice(Math.max(chars(cleaned).length - budget, 0)).join("");
}

function phraseFrom(text: string, hit: { verb: string; index: number }): string {
    const ba = baObjectBefore(text, hit.index);
    const after = text.slice(hit.index + hit.verb.length).replace(/^(?:一份|一个|一下|一下子)/, "").replace(/^关于/, "").trim();
    const withBa = titleWithBaObject(hit.verb, ba, after);
    if (withBa) return withBa;
    if (hit.verb.includes("到")) return hit.verb;
    const dest = after.match(/^[到至]\s*([^\s,，。；;]{1,8})/);
    if (dest) return `${hit.verb}到${dest[1]}`;
    const object = after.split(/[，,。；;]/)[0]?.replace(/^(?:然后|再|并|并且)/, "").trim() || "";
    if (!object) return hit.verb;
    const gap = /^[A-Za-z]/.test(hit.verb) || /^[A-Za-z]/.test(object) ? " " : "";
    const budget = DESCRIBED_TITLE_MAX - chars(hit.verb).length - chars(gap).length;
    return trimDanglingTail(`${hit.verb}${gap}${boundedObject(object, Math.max(budget, 1))}`);
}

function titleWithBaObject(verb: string, ba: string, after: string): string {
    if (!ba) return "";
    const saved = verb.match(/^(.+?)到(.+)$/);
    if (saved) {
        const budget = DESCRIBED_TITLE_MAX - chars(saved[1]).length - chars(saved[2]).length - 1;
        const obj = fitTail(ba, Math.max(budget, 1));
        const gap = /^[A-Za-z]/.test(obj) ? " " : "";
        return `${saved[1]}${gap}${obj}到${saved[2]}`;
    }
    const cheng = after.match(/^成([^\s,，。；;]{1,8})/);
    if (cheng) {
        const head = `${verb}成`;
        const noun = cheng[1];
        const budget = DESCRIBED_TITLE_MAX - chars(head).length - chars(noun).length - 1;
        const obj = fitTail(ba, Math.max(budget, 1));
        const gap = /^[A-Za-z]/.test(obj) ? " " : "";
        return `${head}${gap}${obj}${noun}`;
    }
    const rest = after.replace(/^[好完掉妥]/, "").trim();
    if (rest) return "";
    const gap = /^[A-Za-z]/.test(ba) ? " " : "";
    const budget = DESCRIBED_TITLE_MAX - chars(verb).length - chars(gap).length;
    return `${verb}${gap}${fitTail(ba, Math.max(budget, 1))}`;
}

function boundedObject(object: string, budget: number): string {
    if (chars(object).length <= budget) return object;
    if (/\s/.test(object)) {
        let acc = "";
        for (const word of object.split(/\s+/)) {
            const next = acc ? `${acc} ${word}` : word;
            if (chars(next).length > budget) break;
            acc = next;
        }
        if (acc) return trimDanglingTail(acc);
    }
    return chars(object).slice(0, budget).join("");
}

function trimDanglingTail(text: string): string {
    if (!/\s/.test(text)) return text;
    const words = text.trim().split(/\s+/);
    while (words.length > 1 && /^(?:a|an|the|to|of|for|and|or|with|in|on|at|from|into|by|as)$/i.test(words[words.length - 1] || "")) {
        words.pop();
    }
    return words.join(" ");
}

function actionPhrase(text: string): string {
    const cleaned = stripNoise(text).replace(/^关于/, "").trim();
    const hits = verbHits(cleaned);
    if (hits.length === 0) return "";
    const content = hits.filter((hit) => !SETUP_VERB_RE.test(hit.verb));
    const hit = content.length > 0 ? content[content.length - 1] : hits[hits.length - 1];
    return phraseFrom(cleaned, hit);
}

/** Short display name for a task. Idempotent for names that are already titles. */
export function describeTaskTitle(raw: string): string {
    const collapsed = (raw || "").replace(/\s+/g, " ").trim();
    if (!collapsed) return "";
    if (!looksLikeRawCommand(collapsed)) return collapsed;

    const firstLine = (raw || "").split(/\r?\n/).map((line) => line.trim()).find(Boolean) || collapsed;
    const cleaned = stripNoise(firstLine.replace(/\s+/g, " ").trim());
    if (!cleaned) {
        const redacted = stripNoise(collapsed) || stripHosts(collapsed).replace(CN_SECRET_RE, "").replace(EN_SECRET_RE, "").trim();
        return clip(redacted, DESCRIBED_TITLE_MAX);
    }

    let subject = "";
    let tail = cleaned;
    const colon = cleaned.match(/^([^：:]{1,32})[：:](.+)$/);
    if (colon) {
        subject = prettifySubject(stripNoise(colon[1]));
        tail = colon[2].trim();
    }

    const action = actionPhrase(tail) || actionPhrase(cleaned);
    let title = "";
    if (action && subject) {
        const dest = action.match(/^(.+?)到(.+)$/);
        title = dest ? `${dest[1]} ${subject}到${dest[2]}` : `${action} ${subject}`;
    } else if (action) {
        title = action;
    } else if (subject) {
        title = subject;
    } else {
        title = cleaned.split(/[，,。；;]/)[0]?.trim() || cleaned;
    }

    title = trimDanglingTail(title.replace(/\s+/g, " ").replace(/^[，,：:\s]+|[，,：:\s]+$/g, "").trim());
    if (!title) return clip(cleaned, DESCRIBED_TITLE_MAX);
    return clip(title, DESCRIBED_TITLE_MAX);
}

/** Sidebar row title. Empty when the task has no stored name. */
export function listedTaskTitle(task: { name?: string } | null | undefined): string {
    return describeTaskTitle(String(task?.name || "").trim());
}
