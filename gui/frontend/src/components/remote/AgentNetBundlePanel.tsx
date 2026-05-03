import { useCallback, useEffect, useRef, useState } from "react";
import {
    AgentNetBundleInstall,
    AgentNetBundlePack,
    AgentNetBundleStatus,
    AgentNetBundleUnpack,
} from "../../../wailsjs/go/main/App";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime";
import { cnActionBtn, cnCard, cnHeading, cnInput, cnLabel } from "./agentnetStyles";
import { colors } from "./styles";

const text = (lang: string | undefined, en: string, zhHans: string, zhHant = zhHans) => (
    lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en
);

type Props = { lang: string; agentNetRunning: boolean };

export function AgentNetBundlePanel({ lang, agentNetRunning }: Props) {
    const [installed, setInstalled] = useState<boolean | null>(null);
    const [version, setVersion] = useState("");
    const [busy, setBusy] = useState(false);
    const [message, setMessage] = useState("");
    const [output, setOutput] = useState("");
    const [manualPath, setManualPath] = useState("");
    const [progress, setProgress] = useState<{ stage: string; percent: number; message: string } | null>(null);
    const [packDir, setPackDir] = useState("");
    const [packOut, setPackOut] = useState("");
    const [packPeer, setPackPeer] = useState("");
    const [unpackFile, setUnpackFile] = useState("");
    const [unpackDir, setUnpackDir] = useState("");
    const mountedRef = useRef(true);
    const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            if (timerRef.current) clearTimeout(timerRef.current);
        };
    }, []);

    const showMessage = (value: string, duration = 5000) => {
        if (timerRef.current) clearTimeout(timerRef.current);
        setMessage(value);
        timerRef.current = setTimeout(() => {
            if (mountedRef.current) setMessage("");
        }, duration);
    };

    const refreshStatus = useCallback(async () => {
        if (!agentNetRunning) return;
        try {
            const result = await AgentNetBundleStatus();
            if (!mountedRef.current) return;
            setInstalled(result.installed);
            setVersion(result.version || "");
        } catch {
            if (mountedRef.current) setInstalled(false);
        }
    }, [agentNetRunning]);

    useEffect(() => { refreshStatus(); }, [refreshStatus]);

    useEffect(() => {
        EventsOn("agentnet-bundle-install-progress", (data: any) => {
            if (data && typeof data === "object") {
                setProgress({ stage: data.stage, percent: data.percent ?? 0, message: data.message ?? "" });
                if (data.stage === "done") setTimeout(() => setProgress(null), 1500);
            }
        });
        return () => { EventsOff("agentnet-bundle-install-progress"); };
    }, []);

    const install = async () => {
        setBusy(true);
        setOutput("");
        setManualPath("");
        setProgress({ stage: "downloading", percent: 0, message: text(lang, "Preparing...", "Preparing...") });
        try {
            const result = await AgentNetBundleInstall();
            if (!mountedRef.current) return;
            if (result.ok) {
                showMessage(text(lang, "anet bundle tools installed", "anet bundle tools installed"));
                refreshStatus();
            } else {
                setManualPath(result.manualPath || "");
                showMessage(result.error || text(lang, "Install failed", "Install failed"), 12000);
            }
        } catch (error: any) {
            showMessage(error.message || String(error));
        }
        if (mountedRef.current) {
            setBusy(false);
            setProgress(null);
        }
    };

    const run = async (success: string, action: () => Promise<any>) => {
        setBusy(true);
        setOutput("");
        try {
            const result = await action();
            if (!mountedRef.current) return;
            if (result.ok) {
                showMessage(success);
                if (result.output) setOutput(result.output);
            } else {
                showMessage(result.error || text(lang, "Operation failed", "Operation failed"));
                if (result.output) setOutput(result.output);
            }
        } catch (error: any) {
            showMessage(error.message || String(error));
        }
        if (mountedRef.current) setBusy(false);
    };

    if (!agentNetRunning) return <div style={cnLabel}>{text(lang, "AgentNet not connected", "AgentNet not connected")}</div>;
    if (installed === null) return <div style={cnLabel}>{text(lang, "Checking...", "Checking...")}</div>;

    if (installed === false) {
        return (
            <div style={{ padding: "40px 20px", textAlign: "center" }}>
                <div style={{ fontSize: "0.82rem", fontWeight: 600, color: colors.text, marginBottom: "6px" }}>
                    {text(lang, "anet bundle tools are not installed", "anet bundle tools are not installed")}
                </div>
                <button style={cnActionBtn(busy)} onClick={install} disabled={busy}>
                    {busy ? text(lang, "Installing...", "Installing...") : text(lang, "Install anet", "Install anet")}
                </button>
                {progress && <div style={{ fontSize: "0.68rem", color: colors.textMuted, marginTop: "8px" }}>{progress.message}</div>}
                {message && <div style={{ fontSize: "0.72rem", color: colors.danger, marginTop: "8px" }}>{message}</div>}
                {manualPath && <div style={{ fontFamily: "monospace", fontSize: "0.68rem", color: colors.textSecondary, marginTop: "10px", wordBreak: "break-all" }}>{manualPath}</div>}
            </div>
        );
    }

    return (
        <div style={{ padding: "10px 14px" }}>
            <div style={{ fontSize: "0.65rem", color: colors.textMuted, marginBottom: "8px" }}>anet {version}</div>
            {message && <div style={{ fontSize: "0.72rem", marginBottom: "8px", color: colors.textSecondary }}>{message}</div>}

            <div style={cnCard}>
                <div style={cnHeading}>{text(lang, "Pack .nut", "Pack .nut")}</div>
                <div style={{ marginBottom: "6px" }}>
                    <div style={cnLabel}>{text(lang, "Source directory", "Source directory")}</div>
                    <input value={packDir} onChange={event => setPackDir(event.target.value)} placeholder="./work" style={cnInput} />
                </div>
                <div style={{ marginBottom: "6px" }}>
                    <div style={cnLabel}>{text(lang, "Output file", "Output file")}</div>
                    <input value={packOut} onChange={event => setPackOut(event.target.value)} placeholder="deliverable.nut" style={cnInput} />
                </div>
                <div style={{ marginBottom: "8px" }}>
                    <div style={cnLabel}>{text(lang, "Encrypt for peer (optional)", "Encrypt for peer (optional)")}</div>
                    <input value={packPeer} onChange={event => setPackPeer(event.target.value)} placeholder="12D3KooW..." style={cnInput} />
                </div>
                <button style={cnActionBtn(busy || !packDir.trim() || !packOut.trim())} disabled={busy || !packDir.trim() || !packOut.trim()}
                    onClick={() => run(text(lang, "Packed", "Packed"), () => AgentNetBundlePack(packDir.trim(), packOut.trim(), packPeer.trim()))}>
                    {text(lang, "Pack", "Pack")}
                </button>
            </div>

            <div style={cnCard}>
                <div style={cnHeading}>{text(lang, "Unpack .nut", "Unpack .nut")}</div>
                <div style={{ marginBottom: "6px" }}>
                    <div style={cnLabel}>{text(lang, ".nut file", ".nut file")}</div>
                    <input value={unpackFile} onChange={event => setUnpackFile(event.target.value)} placeholder="deliverable.nut" style={cnInput} />
                </div>
                <div style={{ marginBottom: "8px" }}>
                    <div style={cnLabel}>{text(lang, "Output directory", "Output directory")}</div>
                    <input value={unpackDir} onChange={event => setUnpackDir(event.target.value)} placeholder="./output" style={cnInput} />
                </div>
                <button style={cnActionBtn(busy || !unpackFile.trim())} disabled={busy || !unpackFile.trim()}
                    onClick={() => run(text(lang, "Unpacked", "Unpacked"), () => AgentNetBundleUnpack(unpackFile.trim(), unpackDir.trim() || "./output"))}>
                    {text(lang, "Unpack", "Unpack")}
                </button>
            </div>

            {output && <div style={{ marginTop: "8px", padding: "8px", background: colors.bg, borderRadius: "6px", fontSize: "0.68rem", color: colors.textSecondary, whiteSpace: "pre-wrap", maxHeight: "150px", overflow: "auto", fontFamily: "monospace" }}>{output}</div>}
        </div>
    );
}
