import { useState, useEffect, useCallback, useRef } from "react";
import {
    ClawNetNutshellStatus,
    ClawNetNutshellInstall,
    ClawNetNutshellInit,
    ClawNetNutshellCheck,
    ClawNetNutshellPublish,
    ClawNetNutshellClaim,
    ClawNetNutshellDeliver,
    ClawNetNutshellPack,
    ClawNetNutshellUnpack,
} from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
import { colors } from "./styles";
import { cnCard, cnLabel, cnHeading, cnInput, cnActionBtn, cnTabStyle } from "./agentnetStyles";

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

type Props = { lang: string; clawNetRunning: boolean };

export function ClawNetNutshellPanel({ lang, clawNetRunning }: Props) {
    const [installed, setInstalled] = useState<boolean | null>(null);
    const [version, setVersion] = useState("");
    const [busy, setBusy] = useState(false);
    const [msg, setMsg] = useState("");
    const [output, setOutput] = useState("");
    const [dlProgress, setDlProgress] = useState<{ stage: string; percent: number; message: string } | null>(null);
    const [manualPath, setManualPath] = useState("");
    const [tab, setTab] = useState<"publish" | "claim" | "pack">("publish");

    // Publish form
    const [pubDir, setPubDir] = useState("");
    const [pubReward, setPubReward] = useState(50);

    // Claim form
    const [claimTaskId, setClaimTaskId] = useState("");
    const [claimOutDir, setClaimOutDir] = useState("");

    // Deliver form
    const [deliverDir, setDeliverDir] = useState("");

    // Pack/Unpack form
    const [packDir, setPackDir] = useState("");
    const [packOut, setPackOut] = useState("");
    const [packPeer, setPackPeer] = useState("");
    const [unpackFile, setUnpackFile] = useState("");
    const [unpackDir, setUnpackDir] = useState("");

    const mountedRef = useRef(true);
    const msgTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    useEffect(() => { mountedRef.current = true; return () => { mountedRef.current = false; if (msgTimerRef.current) clearTimeout(msgTimerRef.current); }; }, []);

    const showMsg = (m: string, dur = 5000) => { if (msgTimerRef.current) clearTimeout(msgTimerRef.current); setMsg(m); msgTimerRef.current = setTimeout(() => { if (mountedRef.current) setMsg(""); }, dur); };

    const checkStatus = useCallback(async () => {
        if (!clawNetRunning) return;
        try {
            const res = await ClawNetNutshellStatus();
            if (mountedRef.current) { setInstalled(res.installed); setVersion(res.version || ""); }
        } catch { if (mountedRef.current) setInstalled(false); }
    }, [clawNetRunning]);

    useEffect(() => { checkStatus(); }, [checkStatus]);

    useEffect(() => {
        EventsOn("nutshell-install-progress", (data: any) => {
            if (data && typeof data === "object") {
                setDlProgress({ stage: data.stage, percent: data.percent ?? 0, message: data.message ?? "" });
                if (data.stage === "done") {
                    setTimeout(() => setDlProgress(null), 1500);
                }
            }
        });
        return () => { EventsOff("nutshell-install-progress"); };
    }, []);

    const handleInstall = async () => {
        setBusy(true); setOutput(""); setMsg(""); setManualPath("");
        setDlProgress({ stage: "downloading", percent: 0, message: localizeText(lang, "Preparing...", "准备下载...") });
        try {
            const res = await ClawNetNutshellInstall();
            if (!mountedRef.current) return;
            if (res.ok) { showMsg(localizeText(lang, "✅ Nutshell installed", "✅ Nutshell 已安装")); checkStatus(); }
            else {
                const errStr = res.error || "";
                const isNotAvailable = errStr.includes("nutshell-not-available") || errStr.includes("not available");
                if (isNotAvailable) {
                    setManualPath(res.manualPath || "");
                    showMsg(localizeText(lang,
                        "❌ No prebuilt Nutshell binary for your platform",
                        "❌ 当前平台暂无预编译的 Nutshell 二进制"), 30000);
                } else {
                    showMsg(`❌ ${errStr}`);
                }
            }
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) { setBusy(false); setDlProgress(null); }
    };

    const runAction = async (label: string, fn: () => Promise<any>) => {
        setBusy(true); setOutput(""); setMsg("");
        try {
            const res = await fn();
            if (!mountedRef.current) return;
            if (res.ok) { showMsg(`✅ ${label}`); if (res.output) setOutput(res.output); }
            else { showMsg(`❌ ${res.error}`); if (res.output) setOutput(res.output); }
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) setBusy(false);
    };

    if (!clawNetRunning) return <div style={cnLabel}>{localizeText(lang, "ClawNet not connected", "智网未连接")}</div>;

    if (installed === false) {
        return (
            <div style={{ padding: "40px 20px", textAlign: "center" }}>
                <div style={{ fontSize: "2.5rem", marginBottom: "12px" }}>📦</div>
                <div style={{ fontSize: "0.82rem", fontWeight: 600, color: colors.text, marginBottom: "6px" }}>
                    {localizeText(lang, "Nutshell Not Installed", "Nutshell 未安装")}
                </div>
                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "12px" }}>
                    {localizeText(lang, "Nutshell packages AI task context into .nut bundles", "Nutshell 是 ClawNet 的任务打包工具")}
                </div>

                {/* Progress bar during download */}
                {dlProgress && dlProgress.stage === "downloading" && (
                    <div style={{ margin: "12px auto", maxWidth: "260px" }}>
                        <div style={{ background: colors.bg, borderRadius: "4px", height: "8px", overflow: "hidden", marginBottom: "6px" }}>
                            <div style={{
                                background: colors.primary,
                                height: "100%",
                                width: `${dlProgress.percent}%`,
                                borderRadius: "4px",
                                transition: "width 0.3s ease",
                            }} />
                        </div>
                        <div style={{ fontSize: "0.68rem", color: colors.textMuted }}>{dlProgress.message}</div>
                    </div>
                )}

                <button style={cnActionBtn(busy)} onClick={handleInstall} disabled={busy}>
                    {busy ? localizeText(lang, "Downloading...", "下载中...") : localizeText(lang, "Install Nutshell", "安装 Nutshell")}
                </button>

                {msg && <div style={{ fontSize: "0.72rem", marginTop: "8px", color: msg.startsWith("✅") ? colors.success : colors.danger }}>{msg}</div>}

                {/* Friendly fallback when binary not available for this platform */}
                {manualPath && (
                    <div style={{ marginTop: "12px", padding: "10px", background: colors.bg, borderRadius: "6px", textAlign: "left", fontSize: "0.68rem", color: colors.textSecondary, lineHeight: 1.6 }}>
                        <div style={{ marginBottom: "4px", fontWeight: 600, color: colors.text }}>
                            {localizeText(lang, "💡 Manual Installation", "💡 手动安装方法")}
                        </div>
                        <div>{localizeText(lang, "Download or build the nutshell binary and place it at:", "下载或编译 nutshell 二进制，放到：")}</div>
                        <div style={{ fontFamily: "monospace", fontSize: "0.65rem", padding: "4px 6px", background: colors.accentBg, borderRadius: "4px", margin: "4px 0", wordBreak: "break-all" }}>
                            {manualPath}
                        </div>
                        <div>
                            <a href="https://github.com/ChatChatTech/ClawNet/releases" target="_blank" rel="noopener noreferrer"
                                style={{ color: colors.primary, textDecoration: "underline", cursor: "pointer" }}>
                                GitHub Releases →
                            </a>
                        </div>
                    </div>
                )}
            </div>
        );
    }

    if (installed === null) return <div style={cnLabel}>{localizeText(lang, "Checking...", "检查中...")}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginBottom: "8px" }}>
                📦 Nutshell {version}
            </div>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={cnTabStyle(tab === "publish")} onClick={() => setTab("publish")}>🚀 {localizeText(lang, "Publish", "发布")}</button>
                <button style={cnTabStyle(tab === "claim")} onClick={() => setTab("claim")}>📥 {localizeText(lang, "Claim", "认领")}</button>
                <button style={cnTabStyle(tab === "pack")} onClick={() => setTab("pack")}>📦 {localizeText(lang, "Pack", "打包")}</button>
            </div>
            {msg && <div style={{ fontSize: "0.72rem", marginBottom: "8px", color: msg.startsWith("✅") ? colors.success : colors.danger }}>{msg}</div>}

            {tab === "publish" && (
                <div style={cnCard}>
                    <div style={cnHeading}>🚀 {localizeText(lang, "Publish Bundle", "发布任务包")}</div>
                    <div style={{ marginBottom: "6px" }}>
                        <div style={cnLabel}>{localizeText(lang, "Task directory", "任务目录")}</div>
                        <input value={pubDir} onChange={e => setPubDir(e.target.value)} placeholder="./my-task" style={cnInput} />
                    </div>
                    <div style={{ marginBottom: "8px" }}>
                        <div style={cnLabel}>{localizeText(lang, "Reward (🐚)", "奖励 (🐚)")}</div>
                        <input type="number" value={pubReward} onChange={e => setPubReward(Number(e.target.value))} min={1} style={{ ...cnInput, width: "100px" }} />
                    </div>
                    <div style={{ display: "flex", gap: "6px" }}>
                        <button style={cnActionBtn(busy || !pubDir.trim())} disabled={busy || !pubDir.trim()}
                            onClick={() => runAction(localizeText(lang, "Initialized", "初始化完成"), () => ClawNetNutshellInit(pubDir.trim()))}>
                            {localizeText(lang, "Init", "初始化")}
                        </button>
                        <button style={cnActionBtn(busy || !pubDir.trim())} disabled={busy || !pubDir.trim()}
                            onClick={() => runAction(localizeText(lang, "Check passed", "校验通过"), () => ClawNetNutshellCheck(pubDir.trim()))}>
                            {localizeText(lang, "Check", "校验")}
                        </button>
                        <button style={cnActionBtn(busy || !pubDir.trim())} disabled={busy || !pubDir.trim()}
                            onClick={() => runAction(localizeText(lang, "Published", "已发布"), () => ClawNetNutshellPublish(pubDir.trim(), pubReward))}>
                            {localizeText(lang, "Publish", "发布")}
                        </button>
                    </div>
                </div>
            )}

            {tab === "claim" && (
                <>
                    <div style={cnCard}>
                        <div style={cnHeading}>📥 {localizeText(lang, "Claim Task", "认领任务")}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Task ID", "任务 ID")}</div>
                            <input value={claimTaskId} onChange={e => setClaimTaskId(e.target.value)} placeholder="task-id" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Output directory", "输出目录")}</div>
                            <input value={claimOutDir} onChange={e => setClaimOutDir(e.target.value)} placeholder="./workspace" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !claimTaskId.trim())} disabled={busy || !claimTaskId.trim()}
                            onClick={() => runAction(localizeText(lang, "Claimed", "已认领"), () => ClawNetNutshellClaim(claimTaskId.trim(), claimOutDir.trim() || "./workspace"))}>
                            {localizeText(lang, "Claim", "认领")}
                        </button>
                    </div>
                    <div style={cnCard}>
                        <div style={cnHeading}>📤 {localizeText(lang, "Deliver Work", "提交成果")}</div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Workspace directory", "工作目录")}</div>
                            <input value={deliverDir} onChange={e => setDeliverDir(e.target.value)} placeholder="./workspace" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !deliverDir.trim())} disabled={busy || !deliverDir.trim()}
                            onClick={() => runAction(localizeText(lang, "Delivered", "已提交"), () => ClawNetNutshellDeliver(deliverDir.trim()))}>
                            {localizeText(lang, "Deliver", "提交")}
                        </button>
                    </div>
                </>
            )}

            {tab === "pack" && (
                <>
                    <div style={cnCard}>
                        <div style={cnHeading}>📦 {localizeText(lang, "Pack .nut", "打包 .nut")}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Source directory", "源目录")}</div>
                            <input value={packDir} onChange={e => setPackDir(e.target.value)} placeholder="./my-task" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Output file", "输出文件")}</div>
                            <input value={packOut} onChange={e => setPackOut(e.target.value)} placeholder="task.nut" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Encrypt for peer (optional)", "加密目标 Peer（可选）")}</div>
                            <input value={packPeer} onChange={e => setPackPeer(e.target.value)} placeholder="12D3KooW..." style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !packDir.trim() || !packOut.trim())} disabled={busy || !packDir.trim() || !packOut.trim()}
                            onClick={() => runAction(localizeText(lang, "Packed", "已打包"), () => ClawNetNutshellPack(packDir.trim(), packOut.trim(), packPeer.trim()))}>
                            {localizeText(lang, "Pack", "打包")}
                        </button>
                    </div>
                    <div style={cnCard}>
                        <div style={cnHeading}>📂 {localizeText(lang, "Unpack .nut", "解包 .nut")}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{localizeText(lang, ".nut file", ".nut 文件")}</div>
                            <input value={unpackFile} onChange={e => setUnpackFile(e.target.value)} placeholder="task.nut" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{localizeText(lang, "Output directory", "输出目录")}</div>
                            <input value={unpackDir} onChange={e => setUnpackDir(e.target.value)} placeholder="./output" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !unpackFile.trim())} disabled={busy || !unpackFile.trim()}
                            onClick={() => runAction(localizeText(lang, "Unpacked", "已解包"), () => ClawNetNutshellUnpack(unpackFile.trim(), unpackDir.trim() || "./output"))}>
                            {localizeText(lang, "Unpack", "解包")}
                        </button>
                    </div>
                </>
            )}

            {output && (
                <div style={{ marginTop: "8px", padding: "8px", background: colors.bg, borderRadius: "6px", fontSize: "0.68rem", color: colors.textSecondary, whiteSpace: "pre-wrap", maxHeight: "150px", overflow: "auto", fontFamily: "monospace" }}>
                    {output}
                </div>
            )}
        </div>
    );
}
