// A reply must not claim a file arrived when none reached this chat (Daniel
// 11-12, rule 6). MaClawSrv already corrects a claim when its own attach
// failed (desktop_attach_claim.go); this is the end-to-end check: the words
// say "attached" but no file, image or local path came with the message, so
// something between the bot and this window dropped it.
const ATTACH_CLAIM = /(附在(这条|本条|此)?(消息|回复)|已附上|附上了|随(这条)?(消息|回复)附|附件(里|中|已|在)|见附件|attached (to|in|with) (this|the) (message|reply)|see (the )?attach)/i;

export function desktopBotAttachCorrection(lang: string): string {
    return lang.startsWith('zh')
        ? '（更正：这条消息实际没有收到附件。文件可能在传送途中被丢掉了，可以让我重新发一次，或告诉你文件在哪。）'
        : '(Correction: no file actually arrived with this message. It may have been dropped on the way; ask me to send it again or tell you where it is.)';
}

export function desktopBotCorrectAttachClaim(text: string, delivered: boolean, lang: string): string {
    if (delivered || !text || !ATTACH_CLAIM.test(text)) return text;
    if (/更正|Correction:/.test(text)) return text;
    return text.replace(/\s+$/, '') + '\n' + desktopBotAttachCorrection(lang);
}
