import { describe, expect, it } from "vitest";
import {
    REASONING_BODY_MIN_CHARS,
    REASONING_FOLLOWUP_MAX_CHARS,
    bodyAlreadyHasReasoning,
    cleanReasoningTrailForBody,
    liftReasoningIntoBody,
    looksLikeInternalMonologue,
    mergeReasoningIntoBody,
    parkReplacedStreamInReasoning,
    resolveVisibleAssistantReply,
    separateReasoningFromBody,
    shouldPromoteReasoningToBody,
} from "./assistantReasoningBody";

const longReview = "Major comment: missing scratch-prompt baseline. ".repeat(20).trim();
const shortFollowUp = "评审意见上一条已给出。是否需要英文版 Reviewer Report？";
const cuisineAnswer = [
    "泸州菜和成都菜同属川菜，但风格差异挺明显：",
    "",
    "流派归属",
    "• 成都菜属川菜“上河帮”，讲究精细、调味层次多，是川菜正统的代表。",
    "• 泸州菜偏“小河帮”风格，带浓厚的江湖气息，做法更豪放。",
    "",
    "口味",
    "• 成都菜：麻辣适中，味型丰富多变，如鱼香、家常、怪味、椒麻等，讲究“一菜一格，百菜百味”。",
].join("\n");
const cuisineCot = [
    "The user is asking about the difference between Luzhou cuisine and Chengdu cuisine (both part of Sichuan cuisine). This is a general knowledge question that I can answer without tools. The system prompt says to prefer answering without tools unless live data is needed. This is a cultural/culinary knowledge question that doesn't require live data.",
    "",
    "Let me think about what I know:",
    "",
    "泸州菜 and 成都菜 are both part of 川菜.",
    "成都菜 is part of 上河帮川菜 - more refined, uses more 花椒, moderate spice.",
    "泸州菜 - Luzhou is in southern Sichuan, near the Yangtze River.",
    "I should answer concisely in Chinese. No tools needed - this is general knowledge. Keep it reasonably concise per the \"light\" profile.",
    "",
].join("\n").padEnd(REASONING_BODY_MIN_CHARS + 40, " ");

describe("cleanReasoningTrailForBody", () => {
    it("drops chat status bullets before lift", () => {
        const cleaned = cleanReasoningTrailForBody(`• 已接收任务\n${longReview}`);
        expect(cleaned).toContain("missing scratch-prompt baseline");
        expect(cleaned).not.toContain("已接收任务");
    });

    it("drops Chinese host status bullets from the cuisine-task trail", () => {
        const cleaned = cleanReasoningTrailForBody(
            `• 执行环境已就绪\n• 正在分析任务并开始处理\n${cuisineCot}`,
        );
        expect(cleaned).not.toContain("执行环境已就绪");
        expect(cleaned).not.toContain("正在分析任务并开始处理");
        expect(cleaned).toContain("The user is asking");
    });
});

describe("shouldPromoteReasoningToBody", () => {
    it("promotes empty content with a long reasoning trail", () => {
        expect(longReview.length).toBeGreaterThanOrEqual(REASONING_BODY_MIN_CHARS);
        expect(shouldPromoteReasoningToBody("", longReview)).toBe(true);
    });

    it("promotes a short follow-up when reasoning is the real deliverable", () => {
        expect(shouldPromoteReasoningToBody(shortFollowUp, longReview)).toBe(true);
    });

    it("leaves short thinking next to a real answer in the thinking panel", () => {
        const answer = "今天宁波晴，气温 22 到 28 度，东南风。建议出门带一件薄外套。";
        expect(shouldPromoteReasoningToBody(answer, "Checking the forecast.")).toBe(false);
    });

    it("does not promote a short reasoning-only trail", () => {
        expect(shouldPromoteReasoningToBody("", "Need to persist the report.")).toBe(false);
    });

    it("does not glue a long trail in front of an already-substantial answer", () => {
        const answer = "这是一份已经足够长的正式答复正文，不应当被思考过程覆盖。".repeat(30);
        expect(answer.length).toBeGreaterThanOrEqual(REASONING_BODY_MIN_CHARS);
        expect(shouldPromoteReasoningToBody(answer, longReview)).toBe(false);
        expect(liftReasoningIntoBody(answer, longReview)).toBe(answer);
    });

    it("does not prepend internal CoT onto a concise official answer", () => {
        expect(cuisineAnswer.length).toBeGreaterThan(REASONING_FOLLOWUP_MAX_CHARS);
        expect(cuisineAnswer.length).toBeLessThan(REASONING_BODY_MIN_CHARS);
        expect(looksLikeInternalMonologue(cuisineCot)).toBe(true);
        expect(shouldPromoteReasoningToBody(cuisineAnswer, cuisineCot)).toBe(false);
        expect(liftReasoningIntoBody(cuisineAnswer, cuisineCot)).toBe(cuisineAnswer);
    });
});

describe("mergeReasoningIntoBody", () => {
    it("returns reasoning when content is empty", () => {
        expect(mergeReasoningIntoBody("", longReview)).toBe(longReview);
    });

    it("appends a distinct follow-up after the reasoning trail", () => {
        expect(mergeReasoningIntoBody(shortFollowUp, longReview)).toBe(`${longReview}\n\n${shortFollowUp}`);
    });

    it("does not duplicate when the body already contains the trail", () => {
        const combined = `${longReview}\n\n${shortFollowUp}`;
        expect(mergeReasoningIntoBody(combined, longReview)).toBe(combined);
        expect(bodyAlreadyHasReasoning(combined, longReview)).toBe(true);
        expect(bodyAlreadyHasReasoning(longReview, longReview)).toBe(true);
    });

    it("keeps the trail when it already ends with the follow-up", () => {
        const think = `${longReview}\n\n${shortFollowUp}`;
        expect(mergeReasoningIntoBody(shortFollowUp, think)).toBe(think);
    });

    it("does not drop a polished follow-up that only appears mid-trail", () => {
        const think = `${longReview}\n请告知如何处理。\n${"more analysis. ".repeat(20).trim()}`;
        const body = "请告知如何处理。";
        expect(mergeReasoningIntoBody(body, think)).toBe(`${think}\n\n${body}`);
    });
});

describe("liftReasoningIntoBody", () => {
    it("leaves ordinary answers unchanged", () => {
        expect(liftReasoningIntoBody("It is sunny.", "Checking the forecast.")).toBe("It is sunny.");
    });

    it("lifts a long trail next to a thin follow-up", () => {
        expect(liftReasoningIntoBody(shortFollowUp, longReview)).toContain("missing scratch-prompt baseline");
        expect(liftReasoningIntoBody(shortFollowUp, longReview)).toContain("英文版 Reviewer Report");
    });
});

describe("resolveVisibleAssistantReply", () => {
    it("moves a hidden deliverable into the body and hides the thinking panel", () => {
        const visible = resolveVisibleAssistantReply(shortFollowUp, longReview);
        expect(visible.content).toContain("missing scratch-prompt baseline");
        expect(visible.content).toContain("英文版 Reviewer Report");
        expect(visible.reasoning).toBe("");
    });

    it("keeps live streaming in the thinking panel", () => {
        const visible = resolveVisibleAssistantReply(shortFollowUp, longReview, { live: true });
        expect(visible.content).toBe(shortFollowUp);
        expect(visible.reasoning).toBe(longReview);
    });

    it("strips leading host status from the body while a round is still live", () => {
        const visible = resolveVisibleAssistantReply(
            "• 执行环境已就绪\n今天宁波晴。",
            "Checking the forecast.",
            { live: true },
        );
        expect(visible.content).toBe("今天宁波晴。");
        expect(visible.reasoning).toBe("Checking the forecast.");
    });

    it("hides the thinking panel when the body already contains the trail", () => {
        const combined = `${longReview}\n\n${shortFollowUp}`;
        const visible = resolveVisibleAssistantReply(combined, longReview);
        expect(visible.content).toBe(combined);
        expect(visible.reasoning).toBe("");
    });

    it("leaves ordinary short CoT in the thinking panel", () => {
        const visible = resolveVisibleAssistantReply("It is sunny.", "Checking the forecast.");
        expect(visible.content).toBe("It is sunny.");
        expect(visible.reasoning).toBe("Checking the forecast.");
    });

    it("does not hide short CoT just because the answer quotes a phrase from it", () => {
        const thought = "Checking the forecast.";
        const answer = `Today is sunny. ${thought} Bring a jacket.`;
        const visible = resolveVisibleAssistantReply(answer, thought);
        expect(visible.content).toBe(answer);
        expect(visible.reasoning).toBe(thought);
    });

    it("does not hide thinking when a long trail is only quoted mid-body", () => {
        const answer = `前言。\n\n${longReview}\n\n后记。`;
        const visible = resolveVisibleAssistantReply(answer, longReview);
        expect(visible.content).toBe(answer);
        expect(visible.reasoning).toBe(longReview);
    });

    it("keeps cuisine CoT in the thinking panel and the Chinese answer in the body", () => {
        const visible = resolveVisibleAssistantReply(cuisineAnswer, cuisineCot);
        expect(visible.content).toContain("泸州菜和成都菜同属川菜");
        expect(visible.content).not.toContain("The user is asking");
        expect(visible.content).not.toContain("Let me think");
        expect(visible.reasoning).toContain("The user is asking");
        expect(visible.reasoning).toContain("Let me think");
    });

    it("peels a persisted mix of status, CoT, and official answer", () => {
        const mixed = `• 执行环境已就绪\n• 正在分析任务并开始处理\n${cuisineCot}\n${cuisineAnswer}`;
        const visible = resolveVisibleAssistantReply(mixed, `• 执行环境已就绪\n${cuisineCot}`);
        expect(visible.content.trim()).toBe(cuisineAnswer);
        expect(visible.content).not.toContain("执行环境已就绪");
        expect(visible.content).not.toContain("The user is asking");
        expect(visible.reasoning).toContain("The user is asking");
        expect(visible.reasoning).not.toContain("执行环境已就绪");
    });

    it("splits a reasoning-only monologue that already contains the official answer", () => {
        const trail = `${cuisineCot}\n${cuisineAnswer}`;
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content).toContain("泸州菜和成都菜同属川菜");
        expect(visible.content).not.toContain("The user is asking");
        expect(visible.reasoning).toContain("Let me think");
    });

    it("splits CoT that was dumped into content with an empty reasoning field", () => {
        const mixed = `${cuisineCot}\n${cuisineAnswer}`;
        const visible = resolveVisibleAssistantReply(mixed, "");
        expect(visible.content).toContain("泸州菜和成都菜同属川菜");
        expect(visible.content).not.toContain("The user is asking");
        expect(visible.reasoning).toContain("Let me think");
    });

    it("does not treat an early No tools needed line as the start of the official answer", () => {
        const trail = [
            "The user is asking about Luzhou vs Chengdu cuisine.",
            "No tools needed, this is already known.",
            "",
            "Let me think about river schools, spice levels, and representative dishes. ".repeat(8).trim(),
            "I should answer concisely in Chinese.",
            "",
            cuisineAnswer,
        ].join("\n");
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content).toBe(cuisineAnswer);
        expect(visible.reasoning).toContain("Let me think about river schools");
        expect(visible.reasoning).not.toContain("流派归属");
    });

    it("does not treat an analysis deliverable as internal monologue", () => {
        expect(looksLikeInternalMonologue("I am analyzing the architecture and the missing scratch-prompt baseline.")).toBe(false);
        expect(shouldPromoteReasoningToBody(shortFollowUp, `${longReview}\nI am analyzing the architecture.`)).toBe(true);
    });

    it("does not cut at Chinese notes inside English CoT", () => {
        const trail = [
            "The user is asking about Luzhou vs Chengdu cuisine.",
            "",
            "泸州菜 and 成都菜 are both part of 川菜. Chengdu is 上河帮.",
            "Let me think about spice levels, river schools, and representative dishes. ".repeat(6).trim(),
            "",
            cuisineAnswer,
        ].join("\n");
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content).toBe(cuisineAnswer);
        expect(visible.reasoning).toContain("Let me think about spice levels");
        expect(visible.reasoning).toContain("泸州菜 and 成都菜 are both part of 川菜");
    });

    it("ignores an early I'll answer that still leaves CoT in the remainder", () => {
        const trail = [
            "The user is asking about Luzhou vs Chengdu cuisine.",
            "I'll answer once I finish thinking.",
            "",
            "Let me think about river schools, spice levels, and representative dishes. ".repeat(6).trim(),
            "",
            cuisineAnswer,
        ].join("\n");
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content).toBe(cuisineAnswer);
        expect(visible.reasoning).toContain("Let me think about river schools");
    });

    it("does not treat Let me write as the start of the official answer", () => {
        const trail = [
            "The user is asking me to add a file.",
            "Let me write hello.cpp.",
            "",
            "Need to check the include path, CMake target, and a smoke test. ".repeat(6).trim(),
            "",
            cuisineAnswer,
        ].join("\n");
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content).toBe(cuisineAnswer);
        expect(visible.reasoning).toContain("Let me write hello.cpp.");
        expect(visible.reasoning).toContain("Need to check the include path");
    });

    it("does not split an official English reply that starts with I'll answer", () => {
        const answer = [
            "I'll answer directly.",
            "",
            "Ningbo is sunny today, 22 to 28C, with a southeast breeze. Bring a light jacket if you go out after dusk, and skip the heavy coat.",
        ].join("\n");
        expect(looksLikeInternalMonologue(answer)).toBe(false);
        const visible = resolveVisibleAssistantReply(answer, "");
        expect(visible.content).toBe(answer);
        expect(visible.reasoning).toBe("");
    });

    it("does not cut at 下面给出 inside an already-Chinese official answer", () => {
        const trail = [
            "The user is asking about Luzhou vs Chengdu cuisine.",
            "Let me think about river schools and spice.",
            "I should answer concisely in Chinese.",
            "",
            cuisineAnswer,
            "",
            "下面给出代表菜：",
            "• 古蔺麻辣鸡与泸州白肉都是当地名菜，值得单独一说，不要把这行裁进思考过程。",
        ].join("\n");
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content.startsWith("泸州菜和成都菜同属川菜")).toBe(true);
        expect(visible.content).toContain("下面给出代表菜");
        expect(visible.reasoning).toContain("Let me think about river schools");
        expect(visible.reasoning).not.toContain("流派归属");
    });

    it("splits when the Chinese official answer starts with a markdown heading", () => {
        const headingAnswer = [
            "## 泸州菜和成都菜",
            "",
            "泸州菜偏小河帮，江湖气更重；成都菜属上河帮，调味层次更细。两者同属川菜，但风格差得很明显，不能混为一谈。代表菜也不一样：成都侧是回锅肉、麻婆豆腐，泸州侧是古蔺麻辣鸡、泸州白肉。",
        ].join("\n");
        const trail = [
            "The user is asking about Luzhou vs Chengdu cuisine.",
            "Let me think about river schools, spice levels, and representative dishes. ".repeat(4).trim(),
            "",
            headingAnswer,
        ].join("\n");
        const visible = resolveVisibleAssistantReply("", trail);
        expect(visible.content.startsWith("## 泸州菜和成都菜")).toBe(true);
        expect(visible.content).toContain("两者同属川菜");
        expect(visible.reasoning).toContain("Let me think about river schools");
        expect(visible.reasoning).not.toContain("两者同属川菜");
    });

    it("keeps a live thinking draft inside the panel instead of the answer body", () => {
        const draft = "The user is asking for today's forecast. Let me think through the snippets before answering. The dates disagree, so I should not paste the raw listing.";
        const visible = resolveVisibleAssistantReply(draft, "Checking the forecast.", { live: true });
        expect(visible.content).toBe("");
        expect(visible.reasoning).toContain("The user is asking");
        expect(visible.reasoning).toContain("Checking the forecast.");
    });

    it("keeps a live answer in the bubble when it already follows the plan", () => {
        const plan = "The user is asking for the forecast. Let me think through the snippets before I answer. Several dates in the listing do not match today, so the raw page text cannot be the reply.\n\n";
        const answer = "重庆今天小雨，约 24~30℃，北风，风力不大。明天多云，约 24~31℃，北风。今起三天降雨集中在华西，外出建议带伞，并注意路面积水。空气湿度较高，体感偏闷热。";
        const visible = resolveVisibleAssistantReply(plan + answer, "", { live: true });
        expect(visible.content).toContain("重庆今天小雨");
        expect(visible.reasoning).toContain("The user is asking");
        expect(visible.reasoning).not.toContain("重庆今天小雨");
    });

    it("keeps a tool-call marker in the live answer", () => {
        const plan = "The user is asking for the forecast. Let me think through the snippets before I answer. Several dates in the listing do not match today, so the raw page text cannot be the reply.\n\n";
        const answer = "重庆今天小雨，约 24~30℃，北风，风力不大。明天多云，约 24~31℃，北风。今起三天降雨集中在华西，外出建议带伞，并注意路面积水。空气湿度较高，体感偏闷热。";
        const marked = `${plan}<!--maclaw-tool:call-1-->\n\n${answer}`;
        const visible = resolveVisibleAssistantReply(marked, "", { live: true });
        expect(visible.content).toContain("<!--maclaw-tool:call-1-->");
        expect(visible.content).toContain("重庆今天小雨");
        expect(visible.reasoning).toContain("The user is asking");
        expect(visible.reasoning).not.toContain("maclaw-tool");
        expect(visible.reasoning).not.toContain("重庆今天小雨");
    });
});

describe("parkReplacedStreamInReasoning", () => {
    it("keeps thinking that the final answer replaces", () => {
        const thinking = "The user is asking for Chongqing weather. Let me think through the search snippets and then write a short forecast.";
        const answer = "重庆今天小雨，约 24~30℃，北风。明天多云，约 24~31℃。";
        const kept = parkReplacedStreamInReasoning(thinking, answer, "• 已接收任务");
        expect(kept).toContain("The user is asking");
        expect(kept).toContain("• 已接收任务");
        expect(kept).not.toContain("24~30℃");
    });

    it("does not copy the answer into the thinking panel when nothing was replaced", () => {
        const answer = "重庆今天小雨，约 24~30℃，北风。明天多云，约 24~31℃。外出建议带伞。";
        expect(parkReplacedStreamInReasoning(answer, answer, "Checking the forecast.")).toBe("Checking the forecast.");
    });

    it("does not file a rewritten forecast under thinking", () => {
        const draft = "重庆今天小雨，约 24~30℃，北风。明天多云，约 24~31℃。外出记得带伞，湿度偏高。";
        const answer = "重庆今天小雨，24~30℃。明天多云，24~31℃。建议带伞。";
        expect(parkReplacedStreamInReasoning(draft, answer, "")).toBe("");
    });

    it("keeps a short live forecast in the bubble after the plan", () => {
        const plan = "The user is asking for the forecast. Let me think through the snippets before answering.\n\n";
        const answer = "重庆今天小雨，约 24~30℃，北风不大，建议带伞。";
        const visible = resolveVisibleAssistantReply(plan + answer, "", { live: true });
        expect(visible.content).toContain("重庆今天小雨");
        expect(visible.reasoning).toContain("The user is asking");
        expect(visible.reasoning).not.toContain("建议带伞");
    });

    it("does not keep a search digest that the forecast replaces", () => {
        const digest = "根据公开检索，相关信息如下：\n\n1. 重庆-天气预报\n09/25 周五小雨 北风 30℃ 24℃。09/26 周六多云 北风 31℃ 24℃。今起三天华西有明显降雨，外出注意带伞。";
        const answer = "重庆今天小雨，约 24~30℃，北风。明天多云，约 24~31℃。";
        expect(parkReplacedStreamInReasoning(digest, answer, "")).toBe("");
    });
});

describe("separateReasoningFromBody", () => {
    it("does not lift a hidden review onto the coding-workbench body", () => {
        const separated = separateReasoningFromBody(shortFollowUp, longReview);
        expect(separated.content).toBe(shortFollowUp);
        expect(separated.reasoning).toBe(longReview);
    });

    it("keeps a mid-body quoted status line in the official answer", () => {
        const answer = `${cuisineAnswer}\n有人把“执行环境已就绪”写进了正文，这行必须保留。`;
        const separated = separateReasoningFromBody(
            `• 执行环境已就绪\n${answer}`,
            cuisineCot,
        );
        expect(separated.content).toContain("有人把“执行环境已就绪”写进了正文，这行必须保留。");
        expect(separated.content.startsWith("• 执行环境已就绪")).toBe(false);
        expect(separated.reasoning).toContain("The user is asking");
    });

    it("peels a shorter monologue prefix without waiting for a 600-char trail", () => {
        const cot = [
            "The user is asking about Luzhou vs Chengdu cuisine.",
            "Let me think about the river schools and spice levels.",
            "I should answer concisely in Chinese.",
        ].join("\n");
        expect(cot.length).toBeLessThan(REASONING_BODY_MIN_CHARS);
        const separated = separateReasoningFromBody(`${cot}\n\n${cuisineAnswer}`, cot);
        expect(separated.content).toBe(cuisineAnswer);
        expect(separated.reasoning).toBe(cot);
    });

    it("keeps the longer CoT when content holds a fuller trail than reasoning", () => {
        const shortThink = "The user is asking about cuisine.";
        const separated = separateReasoningFromBody(`${cuisineCot}\n${cuisineAnswer}`, shortThink);
        expect(separated.content).toBe(cuisineAnswer);
        expect(separated.reasoning.length).toBeGreaterThan(shortThink.length);
        expect(separated.reasoning).toContain("Let me think");
    });
});
