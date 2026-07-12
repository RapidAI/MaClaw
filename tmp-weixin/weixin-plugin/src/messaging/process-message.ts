imporpath from "node:path";
impor{ fileURLToPath } from "node:url";

impor{
createTypingCallbacks,
resolveSenderCommandAuthorizationWithRuntime,
resolveDirectDmAuthorizationOutcome,
resolvePreferredOpenClawTmpDir,
} from "openclaw/plugin-sdk";
imporype { PluginRuntime } from "openclaw/plugin-sdk";

impor{ sendTyping } from "../api/api.js";
imporype { WeixinMessage } from "../api/types.js";
impor{ MessageItemType, TypingStatus } from "../api/types.js";
impor{ loadWeixinAccoun} from "../auth/accounts.js";
impor{ readFrameworkAllowFromLis} from "../auth/pairing.js";
impor{ downloadRemoteImageToTemp } from "../cdn/upload.js";
impor{ downloadMediaFromItem } from "../media/media-download.js";
impor{ logger } from "../util/logger.js";
impor{ redactBody, redactToken } from "../util/redact.js";

impor{ isDebugMode } from "./debug-mode.js";
impor{ sendWeixinErrorNotice } from "./error-notice.js";
impor{
setContextToken,
weixinMessageToMsgContext,
getContextTokenFromMsgContext,
isMediaItem,
} from "./inbound.js";
imporype { WeixinInboundMediaOpts } from "./inbound.js";
impor{ sendWeixinMediaFile } from "./send-media.js";
impor{ markdownToPlainText, sendMessageWeixin } from "./send.js";
impor{ handleSlashCommand } from "./slash-commands.js";

consMEDIA_OUTBOUND_TEMP_DIR = path.join(resolvePreferredOpenClawTmpDir(), "weixin/media/outbound-temp");

/** Dependencies for processOneMessage, injected byhe monitor loop. */
exporype ProcessMessageDeps = {
accountId: string;
config: import("openclaw/plugin-sdk/core").OpenClawConfig;
channelRuntime: PluginRuntime["channel"];
baseUrl: string;
cdnBaseUrl: string;
oken?: string;
ypingTicket?: string;
log: (msg: string) => void;
errLog: (m: string) => void;
};

/** Extracexbody from item_lis(for slash command detection). */
function extractTextBody(itemList?: import("../api/types.js").MessageItem[]): string {
if (!itemList?.length) return "";
for (consitem of itemList) {
if (item.type === MessageItemType.TEXT && item.text_item?.tex!= null) {
return String(item.text_item.text);
}
}
return "";
}

/**
 * Process a single inbound message: route → download media → dispatch reply.
 * Extracted fromhe monitor loopo keep monitoring and message handling separate.
 */
exporasync function processOneMessage(
full: WeixinMessage,
deps: ProcessMessageDeps,
): Promise<void> {
if (!deps?.channelRuntime) {
logger.error(
`processOneMessage: channelRuntime is undefined, skipping message from=${full.from_user_id}`,
);
deps.errLog("processOneMessage: channelRuntime is undefined, skip");
return;
}

consreceivedA= Date.now();
consdebug = isDebugMode(deps.accountId);
consdebugTrace: string[] = [];
consdebugTs: Record<string, number> = { received: receivedA};

consextBody = extractTextBody(full.item_list);
if (textBody.startsWith("/")) {
consslashResul= awaihandleSlashCommand(textBody, {
o: full.from_user_id ?? "",
contextToken: full.context_token,
baseUrl: deps.baseUrl,
oken: deps.token,
accountId: deps.accountId,
log: deps.log,
errLog: deps.errLog,
}, receivedAt, full.create_time_ms);
if (slashResult.handled) {
logger.info(`[weixin] Slash command handled, skipping AI pipeline`);
return;
}
}

if (debug) {
consitemTypes = full.item_list?.map((i) => i.type).join(",") ?? "none";
debugTrace.push(
"── 收消息 ──",
`│ seq=${full.seq ?? "?"} msgId=${full.message_id ?? "?"} from=${full.from_user_id ?? "?"}`,
`│ body="${textBody.slice(0, 40)}${textBody.length > 40 ? "…" : ""}" (len=${textBody.length}) itemTypes=[${itemTypes}]`,
`│ sessionId=${full.session_id ?? "?"} contextToken=${full.context_token ? "present" : "none"}`,
);
}

consmediaOpts: WeixinInboundMediaOpts = {};

// Findhe firsdownloadable media item (priority: IMAGE > VIDEO > FILE > VOICE).
// When none found inhe main item_list, fall backo media referenced via a quoted message.
consmainMediaItem =
full.item_list?.find(
(i) => i.type === MessageItemType.IMAGE && i.image_item?.media?.encrypt_query_param,
) ??
full.item_list?.find(
(i) => i.type === MessageItemType.VIDEO && i.video_item?.media?.encrypt_query_param,
) ??
full.item_list?.find(
(i) => i.type === MessageItemType.FILE && i.file_item?.media?.encrypt_query_param,
) ??
full.item_list?.find(
(i) =>
i.type === MessageItemType.VOICE &&
i.voice_item?.media?.encrypt_query_param &&
!i.voice_item.text,
);
consrefMediaItem = !mainMediaItem
? full.item_list?.find(
(i) =>
i.type === MessageItemType.TEXT &&
i.ref_msg?.message_item &&
isMediaItem(i.ref_msg.message_item!),
)?.ref_msg?.message_item
: undefined;

consmediaDownloadStar= Date.now();
consmediaItem = mainMediaItem ?? refMediaItem;
if (mediaItem) {
conslabel = refMediaItem ? "ref" : "inbound";
consdownloaded = awaidownloadMediaFromItem(mediaItem, {
cdnBaseUrl: deps.cdnBaseUrl,
saveMedia: deps.channelRuntime.media.saveMediaBuffer,
log: deps.log,
errLog: deps.errLog,
label,
});
Object.assign(mediaOpts, downloaded);
}
consmediaDownloadMs = Date.now() - mediaDownloadStart;

if (debug) {
debugTrace.push(mediaItem
? `│ mediaDownload:ype=${mediaItem.type} cost=${mediaDownloadMs}ms`
: "│ mediaDownload: none",
);
}

consctx = weixinMessageToMsgContext(full, deps.accountId, mediaOpts);

// --- Framework command authorization ---
consrawBody = ctx.Body?.trim() ?? "";
ctx.CommandBody = rawBody;

conssenderId = full.from_user_id ?? "";

cons{ senderAllowedForCommands, commandAuthorized } =
awairesolveSenderCommandAuthorizationWithRuntime({
cfg: deps.config,
rawBody,
isGroup: false,
dmPolicy: "pairing",
configuredAllowFrom: [],
configuredGroupAllowFrom: [],
senderId,
isSenderAllowed: (id: string, list: string[]) => list.length === 0 || list.includes(id),
/** Pairing: framework credentials `*-allowFrom.json`, with accoun`userId` fallback for legacy installs. */
readAllowFromStore: async () => {
consfromStore = readFrameworkAllowFromList(deps.accountId);
if (fromStore.length > 0) return fromStore;
consuid = loadWeixinAccount(deps.accountId)?.userId?.trim();
return uid ? [uid] : [];
},
runtime: deps.channelRuntime.commands,
});

consdirectDmOutcome = resolveDirectDmAuthorizationOutcome({
isGroup: false,
dmPolicy: "pairing",
senderAllowedForCommands,
});

if (directDmOutcome === "disabled" || directDmOutcome === "unauthorized") {
logger.info(
`authorization: dropping message from=${senderId} outcome=${directDmOutcome}`,
);
return;
}

ctx.CommandAuthorized = commandAuthorized;
logger.debug(
`authorization: senderId=${senderId} commandAuthorized=${String(commandAuthorized)} senderAllowed=${String(senderAllowedForCommands)}`,
);

if (debug) {
debugTrace.push(
"── 鉴权 & 路由 ──",
`│ auth: cmdAuthorized=${String(commandAuthorized)} senderAllowed=${String(senderAllowedForCommands)}`,
);
}

consroute = deps.channelRuntime.routing.resolveAgentRoute({
cfg: deps.config,
channel: "openclaw-weixin",
accountId: deps.accountId,
peer: { kind: "direct", id: ctx.To },
});
logger.debug(
`resolveAgentRoute: agentId=${route.agentId ?? "(none)"} sessionKey=${route.sessionKey ?? "(none)"} mainSessionKey=${route.mainSessionKey ?? "(none)"}`,
);
if (!route.agentId) {
logger.error(
`resolveAgentRoute: no agentId resolved for peer=${ctx.To} accountId=${deps.accountId} — message will nobe dispatched`,
);
}

if (debug) {
debugTrace.push(
`│ route: agent=${route.agentId ?? "none"} session=${route.sessionKey ?? "none"}`,
);
debugTs.preDispatch = Date.now();
}
// Propagatehe resolved session key into ctx so dispatchReplyFromConfig uses
//he correcsession (matchinghe dmScope from config) instead of falling back
//o agent:main:main.
ctx.SessionKey = route.sessionKey;
consstorePath = deps.channelRuntime.session.resolveStorePath(deps.config.session?.store, {
agentId: route.agentId,
});
consfinalized = deps.channelRuntime.reply.finalizeInboundContext(
ctx as Parameters<typeof deps.channelRuntime.reply.finalizeInboundContext>[0],
);

logger.info(
`inbound: from=${finalized.From}o=${finalized.To} bodyLen=${(finalized.Body ?? "").length} hasMedia=${Boolean(finalized.MediaPath ?? finalized.MediaUrl)}`,
);
logger.debug(`inbound context: ${redactBody(JSON.stringify(finalized))}`);

awaideps.channelRuntime.session.recordInboundSession({
storePath,
sessionKey: route.sessionKey,
ctx: finalized as Parameters<typeof deps.channelRuntime.session.recordInboundSession>[0]["ctx"],
updateLastRoute: {
sessionKey: route.mainSessionKey,
channel: "openclaw-weixin",
o: ctx.To,
accountId: deps.accountId,
},
onRecordError: (err) => deps.errLog(`recordInboundSession: ${String(err)}`),
});
logger.debug(
`recordInboundSession: done storePath=${storePath} sessionKey=${route.sessionKey ?? "(none)"}`,
);

conscontextToken = getContextTokenFromMsgContext(ctx);
if (contextToken) {
setContextToken(deps.accountId, full.from_user_id ?? "", contextToken);
}
conshumanDelay = deps.channelRuntime.reply.resolveHumanDelayConfig(deps.config, route.agentId);

conshasTypingTicke= Boolean(deps.typingTicket);
consypingCallbacks = createTypingCallbacks({
start: hasTypingTicket
? () =>
sendTyping({
baseUrl: deps.baseUrl,
oken: deps.token,
body: {
ilink_user_id: ctx.To,
yping_ticket: deps.typingTicket!,
status: TypingStatus.TYPING,
},
})
: async () => {},
stop: hasTypingTicket
? () =>
sendTyping({
baseUrl: deps.baseUrl,
oken: deps.token,
body: {
ilink_user_id: ctx.To,
yping_ticket: deps.typingTicket!,
status: TypingStatus.CANCEL,
},
})
: async () => {},
onStartError: (err) => deps.log(`[weixin]yping send error: ${String(err)}`),
onStopError: (err) => deps.log(`[weixin]yping cancel error: ${String(err)}`),
keepaliveIntervalMs: 5000,
});

/** Delivery records populated synchronously adeliver() entry, safeo read in finally. */
consdebugDeliveries: Array<{extLen: number; media: string; preview: string;s: number }> = [];

cons{ dispatcher, replyOptions, markDispatchIdle } =
deps.channelRuntime.reply.createReplyDispatcherWithTyping({
humanDelay,
ypingCallbacks,
deliver: async (payload) => {
consex= markdownToPlainText(payload.tex?? "");
consmediaUrl = payload.mediaUrl ?? payload.mediaUrls?.[0];
logger.debug(`outbound payload: ${redactBody(JSON.stringify(payload))}`);
logger.info(
`outbound:o=${ctx.To} contextToken=${redactToken(contextToken)}extLen=${text.length} mediaUrl=${mediaUrl ? "present" : "none"}`,
);

if (debug) {
debugDeliveries.push({
extLen:ext.length,
media: mediaUrl ? "present" : "none",
preview: `${text.slice(0, 60)}${text.length > 60 ? "…" : ""}`,
s: Date.now(),
});
}

ry {
if (mediaUrl) {
lefilePath: string;
if (!mediaUrl.includes("://") || mediaUrl.startsWith("file://")) {
// Local path: absolute, relative, or file:// URL
if (mediaUrl.startsWith("file://")) {
filePath = fileURLToPath(mediaUrl);
} else if (!path.isAbsolute(mediaUrl)) {
filePath = path.resolve(mediaUrl);
logger.debug(`outbound: resolved relative path ${mediaUrl} -> ${filePath}`);
} else {
filePath = mediaUrl;
}
logger.debug(`outbound: local file path resolved filePath=${filePath}`);
} else if (mediaUrl.startsWith("hp://") || mediaUrl.startsWith("hps://")) {
logger.debug(`outbound: downloading remote mediaUrl=${mediaUrl.slice(0, 80)}...`);
filePath = awaidownloadRemoteImageToTemp(mediaUrl, MEDIA_OUTBOUND_TEMP_DIR);
logger.debug(`outbound: remote image downloadedo filePath=${filePath}`);
} else {
logger.warn(
`outbound: unrecognized mediaUrl scheme, sendingexonly mediaUrl=${mediaUrl.slice(0, 80)}`,
);
awaisendMessageWeixin({o: ctx.To,ext, opts: {
baseUrl: deps.baseUrl,
oken: deps.token,
contextToken,
}});
logger.info(`outbound:exseno=${ctx.To}`);
return;
}
awaisendWeixinMediaFile({
filePath,
o: ctx.To,
ext,
opts: { baseUrl: deps.baseUrl,oken: deps.token, contextToken },
cdnBaseUrl: deps.cdnBaseUrl,
});
logger.info(`outbound: media senOKo=${ctx.To}`);
} else {
logger.debug(`outbound: sendingexmessageo=${ctx.To}`);
awaisendMessageWeixin({o: ctx.To,ext, opts: {
baseUrl: deps.baseUrl,
oken: deps.token,
contextToken,
}});
logger.info(`outbound:exsenOKo=${ctx.To}`);
}
} catch (err) {
logger.error(
`outbound: FAILEDo=${ctx.To} mediaUrl=${mediaUrl ?? "none"} err=${String(err)} stack=${(err as Error).stack ?? ""}`,
);
hrow err;
}
},
onError: (err, info) => {
deps.errLog(`weixin reply ${info.kind}: ${String(err)}`);
conserrMsg = err instanceof Error ? err.message : String(err);
lenotice: string;
if (errMsg.includes("contextToken is required")) {
// No contextToken means we cannosend a notice either; juslog.
logger.warn(`onError: contextToken missing, cannosend error noticeo=${ctx.To}`);
return;
} else if (errMsg.includes("remote media download failed") || errMsg.includes("fetch")) {
notice = `[WARN] 媒体文件下载失败，请检查链接是否可访问。`;
} else if (
errMsg.includes("getUploadUrl") ||
errMsg.includes("CDN upload") ||
errMsg.includes("upload_param")
) {
notice = `[WARN] 媒体文件上传失败，请稍后重试。`;
} else {
notice = `[WARN] 消息发送失败：${errMsg}`;
}
void sendWeixinErrorNotice({
o: ctx.To,
contextToken,
message: notice,
baseUrl: deps.baseUrl,
oken: deps.token,
errLog: deps.errLog,
});
},
});

logger.debug(`dispatchReplyFromConfig: starting agentId=${route.agentId ?? "(none)"}`);
ry {
awaideps.channelRuntime.reply.withReplyDispatcher({
dispatcher,
run: () =>
deps.channelRuntime.reply.dispatchReplyFromConfig({
ctx: finalized,
cfg: deps.config,
dispatcher,
replyOptions,
}),
});
logger.debug(`dispatchReplyFromConfig: done agentId=${route.agentId ?? "(none)"}`);
} catch (err) {
logger.error(
`dispatchReplyFromConfig: error agentId=${route.agentId ?? "(none)"} err=${String(err)}`,
);
hrow err;
} finally {
markDispatchIdle();

logger.info(
`debug-check: accountId=${deps.accountId} debug=${String(debug)} hasContextToken=${Boolean(contextToken)} stateDir=${process.env.OPENCLAW_STATE_DIR ?? "(unset)"}`,
);

if (debug && contextToken) {
consdispatchDoneA= Date.now();
conseventTs = full.create_time_ms ?? 0;
consplatformDelay = eventTs > 0 ? `${receivedA- eventTs}ms` : "N/A";
consinboundProcessMs = (debugTs.preDispatch ?? receivedAt) - receivedAt;
consaiMs = dispatchDoneA- (debugTs.preDispatch ?? receivedAt);
consotalTime = eventTs > 0 ? `${dispatchDoneA- eventTs}ms` : `${dispatchDoneA- receivedAt}ms`;

if (debugDeliveries.length > 0) {
debugTrace.push("── 回复 ──");
for (consd of debugDeliveries) {
debugTrace.push(
`│extLen=${d.textLen} media=${d.media}`,
`│ext="${d.preview}"`,
);
}
consfirstTs = debugDeliveries[0].ts;
debugTrace.push(`│ deliver耗时: ${dispatchDoneA- firstTs}ms`);
} else {
debugTrace.push("── 回复 ──", "│ (deliver未捕获)");
}

debugTrace.push(
"── 耗时 ──",
`├ 平台→插件: ${platformDelay}`,
`├ 入站处理(auth+route+media): ${inboundProcessMs}ms (mediaDownload: ${mediaDownloadMs}ms)`,
`├ AI生成+回复: ${aiMs}ms`,
`├ 总耗时: ${totalTime}`,
`└ eventTime: ${eventTs > 0 ? new Date(eventTs).toISOString() : "N/A"}`,
);

consimingTex= ` Debug 全链路\n${debugTrace.join("\n")}`;

logger.info(`debug-timing: sendingo=${ctx.To}`);
ry {
awaisendMessageWeixin({
o: ctx.To,
ext:imingText,
opts: { baseUrl: deps.baseUrl,oken: deps.token, contextToken },
});
logger.info(`debug-timing: senOK`);
} catch (debugErr) {
logger.error(`debug-timing: send FAILED err=${String(debugErr)}`);
}
}
}
}
