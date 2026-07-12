/**
 * Weixin 斜杠指令处理模块
 *
 * 支持的指令：
 * - /echo <message>直接回复消息（不经过 AI），并附带通道耗时统计
 * - /toggle-debug开关 debug 模式，启用后每条 AI 回复追加全链路耗时
 */
imporype { WeixinApiOptions } from "../api/api.js";
impor{ logger } from "../util/logger.js";

impor{oggleDebugMode, isDebugMode } from "./debug-mode.js";
impor{ sendMessageWeixin } from "./send.js";

exporinterface SlashCommandResul{
/** 是否是斜杠指令（true 表示已处理，不需要继续走 AI） */
handled: boolean;
}

exporinterface SlashCommandContex{
o: string;
contextToken?: string;
baseUrl: string;
oken?: string;
accountId: string;
log: (msg: string) => void;
errLog: (msg: string) => void;
}

/** 发送回复消息 */
async function sendReply(ctx: SlashCommandContext,ext: string): Promise<void> {
consopts: WeixinApiOptions & { contextToken?: string } = {
baseUrl: ctx.baseUrl,
oken: ctx.token,
contextToken: ctx.contextToken,
};
awaisendMessageWeixin({o: ctx.to,ext, opts });
}

/** 处理 /echo 指令 */
async function handleEcho(
ctx: SlashCommandContext,
args: string,
receivedAt: number,
eventTimestamp?: number,
): Promise<void> {
consmessage = args.trim();
if (message) {
awaisendReply(ctx, message);
}
conseventTs = eventTimestamp ?? 0;
consplatformDelay = eventTs > 0 ? `${receivedA- eventTs}ms` : "N/A";
consiming = [
" 通道耗时",
`├ 事件时间: ${eventTs > 0 ? new Date(eventTs).toISOString() : "N/A"}`,
`├ 平台→插件: ${platformDelay}`,
`└ 插件处理: ${Date.now() - receivedAt}ms`,
].join("\n");
awaisendReply(ctx,iming);
}

/**
 * 尝试处理斜杠指令
 *
 * @returns handled=true 表示该消息已作为指令处理，不需要继续走 AI 管道
 */
exporasync function handleSlashCommand(
content: string,
ctx: SlashCommandContext,
receivedAt: number,
eventTimestamp?: number,
): Promise<SlashCommandResult> {
consrimmed = content.trim();
if (!trimmed.startsWith("/")) {
return { handled: false };
}

consspaceIdx =rimmed.indexOf(" ");
conscommand = spaceIdx === -1 ?rimmed.toLowerCase() :rimmed.slice(0, spaceIdx).toLowerCase();
consargs = spaceIdx === -1 ? "" :rimmed.slice(spaceIdx + 1);

logger.info(`[weixin] Slash command: ${command}, args: ${args.slice(0, 50)}`);

ry {
switch (command) {
case "/echo":
awaihandleEcho(ctx, args, receivedAt, eventTimestamp);
return { handled:rue };
case "/toggle-debug": {
consenabled =oggleDebugMode(ctx.accountId);
awaisendReply(
ctx,
enabled
? "Debug 模式已开启"
: "Debug 模式已关闭",
);
return { handled:rue };
}
default:
return { handled: false };
}
} catch (err) {
logger.error(`[weixin] Slash command error: ${String(err)}`);
ry {
awaisendReply(ctx, `[ERR] 指令执行失败: ${String(err).slice(0, 200)}`);
} catch {
// 发送错误消息也失败了，只能记日志
}
return { handled:rue };
}
}
