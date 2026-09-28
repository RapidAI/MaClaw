import {
    CancelGitHubCopilotOAuth,
    CancelKimiCodeOAuth,
    CancelOpenAIOAuth,
    CancelWorkBuddyOAuth,
    CancelXAIOAuth,
    StartKimiCodeOAuth,
    WaitKimiCodeOAuth,
} from "../../../wailsjs/go/main/App";
import { copyKimiCodeUserCode } from "./LLMConfigOAuthFields";
import { isKimiCodeProvider, isWorkBuddyProvider } from "./providerLogos";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

export function cancelNamedProviderOAuth(providerName?: string) {
    if (providerName === "GitHub Copilot") {
        CancelGitHubCopilotOAuth();
        return;
    }
    if (providerName === "xAI-Grok") {
        void CancelXAIOAuth();
        return;
    }
    if (isWorkBuddyProvider(providerName)) {
        void CancelWorkBuddyOAuth();
        return;
    }
    if (isKimiCodeProvider(providerName)) {
        void CancelKimiCodeOAuth();
        return;
    }
    CancelOpenAIOAuth();
}

export function cancelAllNativeOAuth() {
    CancelOpenAIOAuth();
    void CancelXAIOAuth();
    void CancelWorkBuddyOAuth();
    void CancelKimiCodeOAuth();
}

export function oauthBrowserHelp(name: string, t: Translate): string {
    if (name === "xAI-Grok") {
        return t("Click below to authorize with your xAI account in the browser.", "点击下方按钮，将在浏览器中完成 xAI 账号授权。");
    }
    if (isWorkBuddyProvider(name)) {
        return t("Click below to authorize with your WorkBuddy account in the browser.", "点击下方按钮，将在浏览器中完成 WorkBuddy 账号授权。");
    }
    if (isKimiCodeProvider(name)) {
        return t("Click below to authorize with your Kimi Code account in the browser.", "点击下方按钮，将在浏览器中完成 Kimi Code 账号授权。");
    }
    return t("Click below to authorize with your OpenAI account in the browser.", "点击下方按钮，将在浏览器中完成 OpenAI 账号授权。");
}

export function oauthSignInLabel(name: string, t: Translate): string {
    if (name === "xAI-Grok") return t("Sign in with xAI", "使用 xAI 账号登录");
    if (isWorkBuddyProvider(name)) return t("Sign in with WorkBuddy", "使用 WorkBuddy 账号登录");
    if (isKimiCodeProvider(name)) return t("Sign in with Kimi Code", "使用 Kimi Code 账号登录");
    return t("Sign in with OpenAI", "使用 OpenAI 账号登录");
}

export async function promptKimiCodeDeviceLogin(t: Translate): Promise<{ hint: string; message: string }> {
    const deviceInfo = await StartKimiCodeOAuth();
    const copied = await copyKimiCodeUserCode(deviceInfo.user_code || "");
    const manualURL = deviceInfo.verification_uri_complete || deviceInfo.verification_uri || "";
    const opened = deviceInfo.browser_opened
        ? t("The browser is open. Confirm the Kimi Code login.", "已打开浏览器，请确认 Kimi Code 登录。")
        : t("The browser did not open. Open this page:", "浏览器未能自动打开，请打开此页面：");
    const hint = `${opened}\n${manualURL}\n${t("Code", "验证码")}: ${deviceInfo.user_code}${copied ? `\n${t("The code is on the clipboard.", "验证码已复制。")}` : ""}`;
    return { hint, message: await WaitKimiCodeOAuth() };
}
