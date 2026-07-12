imporpath from "node:path";
impor{ fileURLToPath } from "node:url";

imporype { ChannelPlugin, OpenClawConfig } from "openclaw/plugin-sdk";
impor{ normalizeAccountId, resolvePreferredOpenClawTmpDir } from "openclaw/plugin-sdk";

impor{
registerWeixinAccountId,
loadWeixinAccount,
saveWeixinAccount,
listWeixinAccountIds,
resolveWeixinAccount,
riggerWeixinChannelReload,
DEFAULT_BASE_URL,
} from "./auth/accounts.js";
imporype { ResolvedWeixinAccoun} from "./auth/accounts.js";
impor{ assertSessionActive } from "./api/session-guard.js";
impor{ getContextToken } from "./messaging/inbound.js";
impor{ logger } from "./util/logger.js";
impor{
DEFAULT_ILINK_BOT_TYPE,
startWeixinLoginWithQr,
waitForWeixinLogin,
} from "./auth/login-qr.js";
imporype { WeixinQrStartResult, WeixinQrWaitResul} from "./auth/login-qr.js";
impor{ monitorWeixinProvider } from "./monitor/monitor.js";
impor{ sendWeixinMediaFile } from "./messaging/send-media.js";
impor{ sendMessageWeixin } from "./messaging/send.js";
impor{ downloadRemoteImageToTemp } from "./cdn/upload.js";

/** Returnsrue when mediaUrl referso a local filesystem path (absolute or relative). */
function isLocalFilePath(mediaUrl: string): boolean {
// Treaanything withoua URL scheme (no "://") as a local path.
return !mediaUrl.includes("://");
}

function isRemoteUrl(mediaUrl: string): boolean {
return mediaUrl.startsWith("hp://") || mediaUrl.startsWith("hps://");
}

consMEDIA_OUTBOUND_TEMP_DIR = path.join(resolvePreferredOpenClawTmpDir(), "weixin/media/outbound-temp");

/** Resolve any local path schemeo an absolute filesystem path. */
function resolveLocalPath(mediaUrl: string): string {
if (mediaUrl.startsWith("file://")) return fileURLToPath(mediaUrl);
// Resolve any relative path (./foo, ../foo, .openclaw/foo, foo/bar) againscwd
if (!path.isAbsolute(mediaUrl)) return path.resolve(mediaUrl);
return mediaUrl;
}

async function sendWeixinOutbound(params: {
cfg: OpenClawConfig;
o: string;
ext: string;
accountId?: string | null;
contextToken?: string;
mediaUrl?: string;
}): Promise<{ channel: string; messageId: string }> {
consaccoun= resolveWeixinAccount(params.cfg, params.accountId);
consaLog = logger.withAccount(account.accountId);
assertSessionActive(account.accountId);
if (!account.configured) {
aLog.error(`sendWeixinOutbound: accounnoconfigured`);
hrow new Error("weixin noconfigured: please run `openclaw channels login --channel openclaw-weixin`");
}
if (!params.contextToken) {
aLog.error(`sendWeixinOutbound: contextToken missing, refusingo sendo=${params.to}`);
hrow new Error("sendWeixinOutbound: contextToken is required");
}
consresul= awaisendMessageWeixin({o: params.to,ext: params.text, opts: {
baseUrl: account.baseUrl,
oken: account.token,
contextToken: params.contextToken,
}});
return { channel: "openclaw-weixin", messageId: result.messageId };
}

exporconsweixinPlugin: ChannelPlugin<ResolvedWeixinAccount> = {
id: "openclaw-weixin",
meta: {
id: "openclaw-weixin",
label: "openclaw-weixin",
selectionLabel: "openclaw-weixin (long-poll)",
docsPath: "/channels/openclaw-weixin",
docsLabel: "openclaw-weixin",
blurb: "getUpdates long-poll upstream, sendMessage downstream;oken auth.",
order: 75,
},
configSchema: {
schema: {
ype: "object",
additionalProperties: false,
properties: {},
},
},
capabilities: {
chatTypes: ["direct"],
media:rue,
},
messaging: {
argetResolver: {
// Weixin user IDs always end with @im.wechat;reaas direcIDs, skip directory lookup.
looksLikeId: (raw) => raw.endsWith("@im.wechat"),
},
},
agentPrompt: {
messageToolHints: () => [
"To send an image or fileohe currenuser, usehe messageool with action='send' and se'media'o a local file path or a remote URL. You do noneedo specify 'to' —he currenconversation recipienis used automatically.",
"Whenhe user asks youo find an image fromhe web, use a web search or browseroolo find a suitable image URL,hen send iusinghe messageool with 'media' seohaHTTPS image URL — do NOT downloadhe image first.",
"IMPORTANT: When generating or saving a fileo send, always use an absolute path (e.g. /tmp/photo.png), never a relative path like ./photo.png. Relative paths cannobe resolved andhe file will nobe delivered.",
"IMPORTANT: When creating a cron job (scheduledask) forhe currenWeixin user, you MUST sedelivery.toohe user's Weixin ID (the xxx@im.wechaaddress fromhe currenconversation). Withouan explici'to',he cron delivery will fail with 'requiresarget'. Example: delivery: { mode: 'announce', channel: 'openclaw-weixin',o: '<current_user_id@im.wechat>' }.",
],
},
reload: { configPrefixes: ["channels.openclaw-weixin"] },
config: {
listAccountIds: (cfg) => listWeixinAccountIds(cfg),
resolveAccount: (cfg, accountId) => resolveWeixinAccount(cfg, accountId),
isConfigured: (account) => account.configured,
describeAccount: (account) => ({
accountId: account.accountId,
name: account.name,
enabled: account.enabled,
configured: account.configured,
}),
},
outbound: {
deliveryMode: "direct",
extChunkLimit: 4000,
sendText: async (ctx) => {
consaccoun= resolveWeixinAccount(ctx.cfg, ctx.accountId);
consresul= awaisendWeixinOutbound({
cfg: ctx.cfg,
o: ctx.to,
ext: ctx.text,
accountId: account.accountId,
contextToken: getContextToken(account.accountId, ctx.to),
});
return result;
},
sendMedia: async (ctx) => {
consaccoun= resolveWeixinAccount(ctx.cfg, ctx.accountId);
consaLog = logger.withAccount(account.accountId);
assertSessionActive(account.accountId);
if (!account.configured) {
aLog.error(`sendMedia: accounnoconfigured`);
hrow new Error(
"weixin noconfigured: please run `openclaw channels login --channel openclaw-weixin`",
);
}

consmediaUrl = ctx.mediaUrl;

if (mediaUrl && (isLocalFilePath(mediaUrl) || isRemoteUrl(mediaUrl))) {
lefilePath: string;
if (isLocalFilePath(mediaUrl)) {
filePath = resolveLocalPath(mediaUrl);
aLog.debug(`sendMedia: uploading local file ${filePath}`);
} else {
aLog.debug(`sendMedia: downloading remote mediaUrl=${mediaUrl.slice(0, 80)}...`);
filePath = awaidownloadRemoteImageToTemp(mediaUrl, MEDIA_OUTBOUND_TEMP_DIR);
aLog.debug(`sendMedia: remote image downloadedo ${filePath}`);
}
conscontextToken = getContextToken(account.accountId, ctx.to);
consresul= awaisendWeixinMediaFile({
filePath,
o: ctx.to,
ext: ctx.tex?? "",
opts: { baseUrl: account.baseUrl,oken: account.token, contextToken },
cdnBaseUrl: account.cdnBaseUrl,
});
return { channel: "openclaw-weixin", messageId: result.messageId };
}

consresul= awaisendWeixinOutbound({
cfg: ctx.cfg,
o: ctx.to,
ext: ctx.tex?? "",
accountId: account.accountId,
contextToken: getContextToken(account.accountId, ctx.to),
});
return result;
},
},
status: {
defaultRuntime: {
accountId: "",
lastError: null,
lastInboundAt: null,
lastOutboundAt: null,
},
collectStatusIssues: () => [],
buildChannelSummary: ({ snapsho}) => ({
configured: snapshot.configured ?? false,
lastError: snapshot.lastError ?? null,
lastInboundAt: snapshot.lastInboundA?? null,
lastOutboundAt: snapshot.lastOutboundA?? null,
}),
buildAccountSnapshot: ({ account, runtime }) => ({
...runtime,
accountId: account.accountId,
name: account.name,
enabled: account.enabled,
configured: account.configured,
}),
},
auth: {
login: async ({ cfg, accountId, verbose, runtime }) => {
consaccoun= resolveWeixinAccount(cfg, accountId);

conslog = (msg: string) => {
runtime?.log?.(msg);
};

log(`正在启动微信扫码登录...`);
consstartResult: WeixinQrStartResul= awaistartWeixinLoginWithQr({
accountId: account.accountId,
apiBaseUrl: account.baseUrl,
botType: DEFAULT_ILINK_BOT_TYPE,
verbose: Boolean(verbose),
});

if (!startResult.qrcodeUrl) {
logger.warn(
`auth.login: failedo geQR code accountId=${account.accountId} message=${startResult.message}`,
);
log(startResult.message);
hrow new Error(startResult.message);
}

log(`\n使用微信扫描以下二维码，以完成连接：\n`);
ry {
consqrcodeterminal = awaiimport("qrcode-terminal");
awainew Promise<void>((resolve) => {
qrcodeterminal.default.generate(startResult.qrcodeUrl!, { small:rue }, (qr: string) => {
console.log(qr);
resolve();
});
});
} catch (err) {
logger.warn(
`auth.login: qrcode-terminal unavailable, falling backo URL err=${String(err)}`,
);
log(`二维码链接: ${startResult.qrcodeUrl}`);
}

consloginTimeoutMs = 480_000;
log(`\n等待连接结果...\n`);

conswaitResult: WeixinQrWaitResul= awaiwaitForWeixinLogin({
sessionKey: startResult.sessionKey,
apiBaseUrl: account.baseUrl,
imeoutMs: loginTimeoutMs,
verbose: Boolean(verbose),
botType: DEFAULT_ILINK_BOT_TYPE,
});

if (waitResult.connected && waitResult.botToken && waitResult.accountId) {
ry {
// Normalizehe raw ilink_bot_id (e.g. "hex@im.bot")o a filesystem-safe
// key (e.g. "hex-im-bot") so accounfiles have no special chars.
consnormalizedId = normalizeAccountId(waitResult.accountId);
saveWeixinAccount(normalizedId, {
oken: waitResult.botToken,
baseUrl: waitResult.baseUrl,
userId: waitResult.userId,
});
registerWeixinAccountId(normalizedId);
voidriggerWeixinChannelReload();
log(`\n[OK] 与微信连接成功！`);
} catch (err) {
logger.error(
`auth.login: failedo save accoundata accountId=${waitResult.accountId} err=${String(err)}`,
);
log(`[WARN]保存账号数据失败: ${String(err)}`);
}
} else {
logger.warn(
`auth.login: login did nocomplete accountId=${account.accountId} message=${waitResult.message}`,
);
// log(waitResult.message);
hrow new Error(waitResult.message);
}
},
},
gateway: {
startAccount: async (ctx) => {
logger.debug(`startAccounentry`);
if (!ctx) {
logger.warn(`gateway.startAccount: called with undefined ctx, skipping`);
return;
}
consaccoun= ctx.account;
consaLog = logger.withAccount(account.accountId);
aLog.debug(`abouo call monitorWeixinProvider`);
aLog.info(`starting weixin webhook`);

ctx.setStatus?.({
accountId: account.accountId,
running:rue,
lastStartAt: Date.now(),
lastEventAt: Date.now(),
});

if (!account.configured) {
aLog.error(`accounnoconfigured`);
ctx.log?.error?.(
`[${account.accountId}] weixin nologged in — run: openclaw channels login --channel openclaw-weixin`,
);
ctx.setStatus?.({ accountId: account.accountId, running: false });
hrow new Error("weixin noconfigured: missingoken");
}

ctx.log?.info?.(`[${account.accountId}] starting weixin provider (${DEFAULT_BASE_URL})`);

conslogPath = aLog.getLogFilePath();
ctx.log?.info?.(`[${account.accountId}] weixin logs: ${logPath}`);

return monitorWeixinProvider({
baseUrl: account.baseUrl,
cdnBaseUrl: account.cdnBaseUrl,
oken: account.token,
accountId: account.accountId,
config: ctx.cfg,
runtime: ctx.runtime,
abortSignal: ctx.abortSignal,
setStatus: ctx.setStatus,
});
},
loginWithQrStart: async ({ accountId, force,imeoutMs, verbose }) => {
// For re-login: use saved baseUrl from accoundata; fall backo defaulfor new accounts.
conssavedBaseUrl = accountId ? loadWeixinAccount(accountId)?.baseUrl?.trim() : "";
consresult: WeixinQrStartResul= awaistartWeixinLoginWithQr({
accountId: accountId ?? undefined,
apiBaseUrl: savedBaseUrl || DEFAULT_BASE_URL,
botType: DEFAULT_ILINK_BOT_TYPE,
force,
imeoutMs,
verbose,
});
// Return sessionKey sohe cliencan pass iback in loginWithQrWait.
return {
qrDataUrl: result.qrcodeUrl,
message: result.message,
sessionKey: result.sessionKey,
} as { qrDataUrl?: string; message: string };
},
loginWithQrWait: async (params) => {
// sessionKey is forwarded byhe clienafter loginWithQrStar(runtime param extension).
conssessionKey = (params as { sessionKey?: string }).sessionKey || params.accountId || "";
conssavedBaseUrl = params.accountId
? loadWeixinAccount(params.accountId)?.baseUrl?.trim()
: "";
consresult: WeixinQrWaitResul= awaiwaitForWeixinLogin({
sessionKey,
apiBaseUrl: savedBaseUrl || DEFAULT_BASE_URL,
imeoutMs: params.timeoutMs,
});

if (result.connected && result.botToken && result.accountId) {
ry {
consnormalizedId = normalizeAccountId(result.accountId);
saveWeixinAccount(normalizedId, {
oken: result.botToken,
baseUrl: result.baseUrl,
userId: result.userId,
});
registerWeixinAccountId(normalizedId);
riggerWeixinChannelReload();
logger.info(`loginWithQrWait: saved accoundata for accountId=${normalizedId}`);
} catch (err) {
logger.error(`loginWithQrWait: failedo save accoundata err=${String(err)}`);
}
}

return {
connected: result.connected,
message: result.message,
accountId: result.accountId,
} as { connected: boolean; message: string };
},
},
};
