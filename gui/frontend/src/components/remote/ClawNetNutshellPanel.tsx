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
import { colors } from "./styles";
import { cnCard, cnLabel, cnHeading, cnInput, cnActionBtn, cnTabStyle } from "./clawnetStyles";

type Props = { lang: string; clawNetRunning: boolean };

export function ClawNetNutshellPanel({ lang, clawNetRunning }: Props) {
    const zh = lang?.startsWith("zh");
    const [installed, setInstalled] = useState<boolean | null>(null);
    const [version, setVersion] = useState("");
    const [busy, setBusy] = useState(false);
    const [msg, setMsg] = useState("");
    const [output, setOutput] = useState("");
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

    const handleInstall = async () => {
        setBusy(true); setOutput("");
        try {
            const res = await ClawNetNutshellInstall();
            if (!mountedRef.current) return;
            if (res.ok) { showMsg(zh ? "✅ Nutshell 已安装" : "✅ Nutshell installed"); checkStatus(); }
            else showMsg(`❌ ${res.error}`);
        } catch (e: any) { showMsg(`❌ ${e.message}`); }
        if (mountedRef.current) setBusy(false);
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

    if (!clawNetRunning) return <div style={cnLabel}>{zh ? "虾网未连接" : "ClawNet not connected"}</div>;

    if (installed === false) {
        return (
            <div style={{ padding: "40px 20px", textAlign: "center" }}>
                <div style={{ fontSize: "2.5rem", marginBottom: "12px" }}>📦</div>
                <div style={{ fontSize: "0.82rem", fontWeight: 600, color: colors.text, marginBottom: "6px" }}>
                    {zh ? "Nutshell 未安装" : "Nutshell Not Installed"}
                </div>
                <div style={{ fontSize: "0.72rem", color: colors.textMuted, marginBottom: "12px" }}>
                    {zh ? "Nutshell 是 ClawNet 的任务打包工具" : "Nutshell packages AI task context into .nut bundles"}
                </div>
                <button style={cnActionBtn(busy)} onClick={handleInstall} disabled={busy}>
                    {busy ? "..." : (zh ? "安装 Nutshell" : "Install Nutshell")}
                </button>
                {msg && <div style={{ fontSize: "0.72rem", marginTop: "8px", color: msg.startsWith("✅") ? colors.success : colors.danger }}>{msg}</div>}
            </div>
        );
    }

    if (installed === null) return <div style={cnLabel}>{zh ? "检查中..." : "Checking..."}</div>;

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginBottom: "8px" }}>
                📦 Nutshell {version}
            </div>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={cnTabStyle(tab === "publish")} onClick={() => setTab("publish")}>🚀 {zh ? "发布" : "Publish"}</button>
                <button style={cnTabStyle(tab === "claim")} onClick={() => setTab("claim")}>📥 {zh ? "认领" : "Claim"}</button>
                <button style={cnTabStyle(tab === "pack")} onClick={() => setTab("pack")}>📦 {zh ? "打包" : "Pack"}</button>
            </div>
            {msg && <div style={{ fontSize: "0.72rem", marginBottom: "8px", color: msg.startsWith("✅") ? colors.success : colors.danger }}>{msg}</div>}

            {tab === "publish" && (
                <div style={cnCard}>
                    <div style={cnHeading}>🚀 {zh ? "发布任务包" : "Publish Bundle"}</div>
                    <div style={{ marginBottom: "6px" }}>
                        <div style={cnLabel}>{zh ? "任务目录" : "Task directory"}</div>
                        <input value={pubDir} onChange={e => setPubDir(e.target.value)} placeholder="./my-task" style={cnInput} />
                    </div>
                    <div style={{ marginBottom: "8px" }}>
                        <div style={cnLabel}>{zh ? "奖励 (🐚)" : "Reward (🐚)"}</div>
                        <input type="number" value={pubReward} onChange={e => setPubReward(Number(e.target.value))} min={1} style={{ ...cnInput, width: "100px" }} />
                    </div>
                    <div style={{ display: "flex", gap: "6px" }}>
                        <button style={cnActionBtn(busy || !pubDir.trim())} disabled={busy || !pubDir.trim()}
                            onClick={() => runAction(zh ? "初始化完成" : "Initialized", () => ClawNetNutshellInit(pubDir.trim()))}>
                            {zh ? "初始化" : "Init"}
                        </button>
                        <button style={cnActionBtn(busy || !pubDir.trim())} disabled={busy || !pubDir.trim()}
                            onClick={() => runAction(zh ? "校验通过" : "Check passed", () => ClawNetNutshellCheck(pubDir.trim()))}>
                            {zh ? "校验" : "Check"}
                        </button>
                        <button style={cnActionBtn(busy || !pubDir.trim())} disabled={busy || !pubDir.trim()}
                            onClick={() => runAction(zh ? "已发布" : "Published", () => ClawNetNutshellPublish(pubDir.trim(), pubReward))}>
                            {zh ? "发布" : "Publish"}
                        </button>
                    </div>
                </div>
            )}

            {tab === "claim" && (
                <>
                    <div style={cnCard}>
                        <div style={cnHeading}>📥 {zh ? "认领任务" : "Claim Task"}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "任务 ID" : "Task ID"}</div>
                            <input value={claimTaskId} onChange={e => setClaimTaskId(e.target.value)} placeholder="task-id" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{zh ? "输出目录" : "Output directory"}</div>
                            <input value={claimOutDir} onChange={e => setClaimOutDir(e.target.value)} placeholder="./workspace" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !claimTaskId.trim())} disabled={busy || !claimTaskId.trim()}
                            onClick={() => runAction(zh ? "已认领" : "Claimed", () => ClawNetNutshellClaim(claimTaskId.trim(), claimOutDir.trim() || "./workspace"))}>
                            {zh ? "认领" : "Claim"}
                        </button>
                    </div>
                    <div style={cnCard}>
                        <div style={cnHeading}>📤 {zh ? "提交成果" : "Deliver Work"}</div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{zh ? "工作目录" : "Workspace directory"}</div>
                            <input value={deliverDir} onChange={e => setDeliverDir(e.target.value)} placeholder="./workspace" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !deliverDir.trim())} disabled={busy || !deliverDir.trim()}
                            onClick={() => runAction(zh ? "已提交" : "Delivered", () => ClawNetNutshellDeliver(deliverDir.trim()))}>
                            {zh ? "提交" : "Deliver"}
                        </button>
                    </div>
                </>
            )}

            {tab === "pack" && (
                <>
                    <div style={cnCard}>
                        <div style={cnHeading}>📦 {zh ? "打包 .nut" : "Pack .nut"}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "源目录" : "Source directory"}</div>
                            <input value={packDir} onChange={e => setPackDir(e.target.value)} placeholder="./my-task" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? "输出文件" : "Output file"}</div>
                            <input value={packOut} onChange={e => setPackOut(e.target.value)} placeholder="task.nut" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{zh ? "加密目标 Peer（可选）" : "Encrypt for peer (optional)"}</div>
                            <input value={packPeer} onChange={e => setPackPeer(e.target.value)} placeholder="12D3KooW..." style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !packDir.trim() || !packOut.trim())} disabled={busy || !packDir.trim() || !packOut.trim()}
                            onClick={() => runAction(zh ? "已打包" : "Packed", () => ClawNetNutshellPack(packDir.trim(), packOut.trim(), packPeer.trim()))}>
                            {zh ? "打包" : "Pack"}
                        </button>
                    </div>
                    <div style={cnCard}>
                        <div style={cnHeading}>📂 {zh ? "解包 .nut" : "Unpack .nut"}</div>
                        <div style={{ marginBottom: "6px" }}>
                            <div style={cnLabel}>{zh ? ".nut 文件" : ".nut file"}</div>
                            <input value={unpackFile} onChange={e => setUnpackFile(e.target.value)} placeholder="task.nut" style={cnInput} />
                        </div>
                        <div style={{ marginBottom: "8px" }}>
                            <div style={cnLabel}>{zh ? "输出目录" : "Output directory"}</div>
                            <input value={unpackDir} onChange={e => setUnpackDir(e.target.value)} placeholder="./output" style={cnInput} />
                        </div>
                        <button style={cnActionBtn(busy || !unpackFile.trim())} disabled={busy || !unpackFile.trim()}
                            onClick={() => runAction(zh ? "已解包" : "Unpacked", () => ClawNetNutshellUnpack(unpackFile.trim(), unpackDir.trim() || "./output"))}>
                            {zh ? "解包" : "Unpack"}
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
