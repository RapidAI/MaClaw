import {
    CancelGitHubCopilotOAuth,
    CancelKimiCodeOAuth,
    CancelOpenAIOAuth,
    CancelQoderOAuth,
    CancelWorkBuddyOAuth,
    CancelXAIOAuth,
    CompleteAnthropicOAuth,
    StartAnthropicOAuth,
    StartGitHubCopilotOAuth,
    StartKimiCodeOAuth,
    StartOpenAIOAuth,
    StartQoderOAuth,
    StartWorkBuddyOAuth,
    StartXAIOAuth,
    WaitGitHubCopilotOAuth,
    WaitKimiCodeOAuth,
    WaitQoderOAuth,
} from "../../../wailsjs/go/main/App";
import { copyKimiCodeUserCode } from "./LLMConfigOAuthFields";
import { isKimiCodeProvider, isQoderProvider, isWorkBuddyProvider } from "./providerLogos";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

/** Edition sent to StartQoderOAuth for the 国内/国际 pair. */
export function qoderEdition(providerName?: string): string | null {
    if (!isQoderProvider(providerName)) return null;
    return providerName === "Qoder 国际版" ? "global" : "cn";
}

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
    if (isQoderProvider(providerName)) {
        void CancelQoderOAuth();
        return;
    }
    CancelOpenAIOAuth();
}

export function cancelAllNativeOAuth() {
    CancelOpenAIOAuth();
    void CancelXAIOAuth();
    void CancelWorkBuddyOAuth();
    void CancelKimiCodeOAuth();
    void CancelQoderOAuth();
}

export function oauthBrowserHelp(name: string, t: Translate): string {
    if (name === "xAI-Grok") {
        return t("Click below to authorize with your xAI account in the browser. After you approve, MaClaw finishes automatically.", "点击下方按钮，将在浏览器中完成 xAI 账号授权。允许后这里会自动完成，不用复制验证码。");
    }
    if (isWorkBuddyProvider(name)) {
        return t("Click below to authorize with your WorkBuddy account in the browser.", "点击下方按钮，将在浏览器中完成 WorkBuddy 账号授权。");
    }
    if (isKimiCodeProvider(name)) {
        return t("Click below to authorize with your Kimi Code account in the browser.", "点击下方按钮，将在浏览器中完成 Kimi Code 账号授权。");
    }
    if (isQoderProvider(name)) {
        return t("Click below to authorize with your Qoder account in the browser. After you approve, MaClaw finishes automatically.", "点击下方按钮，将在浏览器中打开 Qoder 授权页。登录并允许后这里会自动完成，无需复制验证码。");
    }
    return t("Click below to authorize with your OpenAI account in the browser.", "点击下方按钮，将在浏览器中完成 OpenAI 账号授权。");
}

export function oauthSignInLabel(name: string, t: Translate): string {
    if (name === "xAI-Grok") return t("Sign in with xAI", "使用 xAI 账号登录");
    if (isWorkBuddyProvider(name)) return t("Sign in with WorkBuddy", "使用 WorkBuddy 账号登录");
    if (isKimiCodeProvider(name)) return t("Sign in with Kimi Code", "使用 Kimi Code 账号登录");
    if (isQoderProvider(name)) return t("Sign in with Qoder", "使用 Qoder 账号登录");
    return t("Sign in with OpenAI", "使用 OpenAI 账号登录");
}

/**
 * Starts the Kimi Code device-code login. The hint — which carries the device
 * code the user must type into the browser — is delivered through onHint as
 * soon as the device session exists, NOT after the wait resolves: the user can
 * only finish the browser login after seeing the code, so awaiting the wait
 * first would deadlock the flow (the code would never be displayed).
 *
 * Resolves with the login message once WaitKimiCodeOAuth completes.
 */
export async function promptKimiCodeDeviceLogin(t: Translate, onHint: (hint: string) => void): Promise<string> {
    const deviceInfo = await StartKimiCodeOAuth();
    const copied = await copyKimiCodeUserCode(deviceInfo.user_code || "");
    const manualURL = deviceInfo.verification_uri_complete || deviceInfo.verification_uri || "";
    const opened = deviceInfo.browser_opened
        ? t("The browser is open. Confirm the Kimi Code login.", "已打开浏览器，请确认 Kimi Code 登录。")
        : t("The browser did not open. Open this page:", "浏览器未能自动打开，请打开此页面：");
    onHint(`${opened}\n${manualURL}\n${t("Code", "验证码")}: ${deviceInfo.user_code}${copied ? `\n${t("The code is on the clipboard.", "验证码已复制。")}` : ""}`);
    return WaitKimiCodeOAuth();
}

/**
 * Runs the Qoder browser device login. The approval page opens on its own;
 * the hint carries the URL for when the browser could not be opened. Resolves
 * with the login message once WaitQoderOAuth completes.
 */
export async function promptQoderBrowserLogin(providerName: string, t: Translate, onHint: (hint: string) => void): Promise<string> {
    const edition = qoderEdition(providerName);
    if (!edition) throw new Error(t("Unknown Qoder edition", "未知的 Qoder 版本"));
    const info = await StartQoderOAuth(edition);
    const manualURL = info.auth_url || "";
    const opened = info.browser_opened
        ? t("The browser is open. Confirm the Qoder login.", "已打开浏览器，请确认 Qoder 登录。")
        : t("The browser did not open. Open this page:", "浏览器未能自动打开，请打开此页面：");
    onHint(`${opened}\n${manualURL}`);
    return WaitQoderOAuth();
}

type OAuthTestResult = { ok: boolean; msg: string; retryable?: boolean };
type OAuthPrompt = (message: string, title?: string, options?: { placeholder?: string }) => Promise<string | null>;

/**
 * Runs the provider-specific OAuth login flow and resolves with the login
 * message. Resolves `null` when the user cancels mid-flow (e.g. dismissing
 * the Anthropic authorization-code prompt) — the caller must treat `null`
 * as "nothing happened, skip the post-login bookkeeping".
 *
 * Stale-attempt guards are the caller's responsibility: the panel owns the
 * oauthAttemptRef bookkeeping, so it checks staleness once after this
 * resolves and inside onDeviceHint (the device code must never paint over
 * a live attempt's hint).
 */
export async function runProviderOAuthLogin(options: {
    providerName: string;
    t: Translate;
    showPrompt: OAuthPrompt;
    onDeviceHint: (hint: string) => void;
    setTestResult: (result: OAuthTestResult | null) => void;
}): Promise<string | null> {
    const { providerName, t, showPrompt, onDeviceHint, setTestResult } = options;

    if (providerName === "Anthropic") {
        const info = await StartAnthropicOAuth();
        window.open(info.auth_url, "_blank");
        const code = await showPrompt(
            t(
                "Please paste the Authorization Code shown in the browser page:",
                "请粘贴浏览器页面中显示的授权码 (Authorization Code):",
            ),
            t("Authorization Code", "授权码"),
            {
                placeholder: t("Paste authorization code here", "在此粘贴授权码"),
            },
        );
        if (!code?.trim()) {
            setTestResult({ ok: false, msg: t("Cancelled", "已取消") });
            return null;
        }
        return CompleteAnthropicOAuth(code.trim());
    }
    if (providerName === "GitHub Copilot") {
        const deviceInfo = await StartGitHubCopilotOAuth();
        setTestResult({
            ok: true,
            msg: `请打开 ${deviceInfo.verification_uri} 并输入代码: ${deviceInfo.user_code}`,
        });
        return WaitGitHubCopilotOAuth();
    }
    if (providerName === "xAI-Grok") {
        // The browser opens on accounts.x.ai with the approval already filled in.
        // MaClaw polls until the user allows it, so nothing is pasted back here.
        onDeviceHint(t(
            "The browser is opening. Approve the login there and MaClaw finishes by itself. Do not paste a code.",
            "正在打开浏览器。在页面上点允许即可，这里会自动完成，不用把验证码贴回来。",
        ));
        return StartXAIOAuth();
    }
    if (isWorkBuddyProvider(providerName)) {
        return StartWorkBuddyOAuth(providerName);
    }
    if (isKimiCodeProvider(providerName)) {
        return promptKimiCodeDeviceLogin(t, onDeviceHint);
    }
    if (isQoderProvider(providerName)) {
        return promptQoderBrowserLogin(providerName, t, onDeviceHint);
    }
    return StartOpenAIOAuth();
}
