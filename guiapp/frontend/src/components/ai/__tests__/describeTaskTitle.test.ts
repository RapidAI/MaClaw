import { describe, expect, it } from "vitest";
import { describeTaskTitle } from "../describeTaskTitle";

describe("describeTaskTitle", () => {
    it("rewrites a parameter dump into a short action and subject", () => {
        const title = describeTaskTitle("api2服务器信息：api2.maclaw.top root hunter2, 保存到知识库");
        expect(title).toBe("保存 API2 服务器信息到知识库");
        expect(title).not.toMatch(/hunter2|maclaw|root/i);
    });

    it("keeps an already short task name", () => {
        expect(describeTaskTitle("查看驱网信息")).toBe("查看驱网信息");
        expect(describeTaskTitle("查看服务器状态")).toBe("查看服务器状态");
        expect(describeTaskTitle("Linux系统信息工具")).toBe("Linux系统信息工具");
        expect(describeTaskTitle("修复登录 bug")).toBe("修复登录 bug");
        expect(describeTaskTitle("继续")).toBe("继续");
        expect(describeTaskTitle("帮我写周报")).toBe("帮我写周报");
    });

    it("drops secrets even when the rest of the line is an instruction", () => {
        const title = describeTaskTitle("连接 10.0.0.8 用户 admin 密码 hunter2，然后备份数据库");
        expect(title).not.toMatch(/hunter2|10\.0\.0\.8|admin/);
        expect(title).toContain("备份");
    });

    it("uses only the first line and stays short", () => {
        const body = `第一行很长${"字".repeat(90)}\n第二行也要留下`;
        const title = describeTaskTitle(body);
        expect(title.startsWith("第一行很长")).toBe(true);
        expect(title.endsWith("…")).toBe(true);
        expect([...title].length).toBeLessThanOrEqual(25);
        expect(title).not.toContain("第二行");
    });

    it("is idempotent", () => {
        const once = describeTaskTitle("api2服务器信息：api2.maclaw.top root hunter2, 保存到知识库");
        expect(describeTaskTitle(once)).toBe(once);
        expect(describeTaskTitle("查看服务器状态")).toBe("查看服务器状态");
    });

    it("leaves paths alone", () => {
        const path = "C:\\Users\\me\\maclaw\\data\\tasks\\api2服务器信息-1234567890";
        expect(describeTaskTitle(path)).toBe(path);
    });

    it("keeps ordinary names that merely contain 用户, a file suffix, or tokenizer", () => {
        expect(describeTaskTitle("查看用户列表")).toBe("查看用户列表");
        expect(describeTaskTitle("notes.pdf")).toBe("notes.pdf");
        expect(describeTaskTitle("修复 Node.js 构建")).toBe("修复 Node.js 构建");
        expect(describeTaskTitle("检查 tokenizer 配置")).toBe("检查 tokenizer 配置");
    });

    it("keeps a token-refresh topic instead of treating the next clause as a secret", () => {
        const title = describeTaskTitle("请把 access token 刷新流程写成文档，包含过期和续期两节说明");
        expect(title).toContain("刷新");
        expect(title).toContain("文档");
        expect(title).not.toBe("写成文档");
        expect(title).not.toMatch(/hunter2|password/i);
    });

    it("does not match an English verb inside a longer word, and keeps the object", () => {
        const prefixed = describeTaskTitle("Please prefix every log line with the request id and write the result to the knowledge base");
        expect(prefixed).not.toBe("write");
        expect(prefixed.startsWith("fix")).toBe(false);
        expect(prefixed.toLowerCase()).toContain("write");
        expect(prefixed).not.toMatch(/\b(?:to|the|of|for|and)$/i);
        const checksum = describeTaskTitle("verify the checksum and write a long incident report for the outage");
        expect(checksum).not.toBe("write");
        expect(checksum.toLowerCase()).toContain("incident");
    });

    it("keeps the 把 object with the verb", () => {
        const title = describeTaskTitle("请把本周销售周报整理成三页说明，并补充给管理层的结论、风险和后续安排");
        expect(title).toContain("周报");
        expect(title).toContain("整理");
        expect(title.startsWith("请把")).toBe(false);
    });

    it("does not treat 写 inside 编写 as the action", () => {
        const title = describeTaskTitle("请帮我编写一份很长的项目周报说明，包含图表、结论以及下周的详细计划安排");
        expect(title.startsWith("写")).toBe(false);
        expect(title).toContain("周报");
    });
});
