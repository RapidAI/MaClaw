import React from "react";
import { createRoot } from "react-dom/client";
import { AssistantMermaidDiagram } from "../src/components/ai/AssistantMermaidDiagram";
import { lightTheme } from "../src/components/ai/aiAssistantPanelTheme";

// The diagram from the reader's screenshot: small enough that "natural size"
// looks lost in a wide chat column.
const CODE = `flowchart TD
    A["KV Cache 压缩"] --> B["CD+CSA 压缩"]
    A --> C["FP8 Muon KV"]
    B --> D["1/4 全局 KV"]
    C --> E["1/8 持久 KV"]
    A --> F["Disk-Bounded Replay"]`;

const PANEL_WIDTH = 560;

function Panel({ id, label, note }: { id: string; label: string; note: string }) {
    return (
        <div style={{ width: PANEL_WIDTH }}>
            <div style={{ font: "600 14px 'Segoe UI', system-ui", color: "#0f172a" }}>{label}</div>
            <div style={{ font: "400 12px 'Segoe UI', system-ui", color: "#64748b", marginBottom: 6 }}>{note}</div>
            <div style={{ width: 200, height: 8, background: "#dc2626" }} />
            <div style={{ font: "400 11px 'Segoe UI', system-ui", color: "#64748b" }}>↑ 这段红条 = 200 CSS px 标尺</div>
            <div id={id} style={{ border: "1px solid #d8dee9", borderRadius: 10, padding: 12, background: "#ffffff" }}>
                <AssistantMermaidDiagram code={CODE} theme={lightTheme} />
            </div>
        </div>
    );
}

createRoot(document.getElementById("root")!).render(
    <>
        <div
            id="diag"
            style={{
                padding: "10px 24px 0",
                font: "500 15px ui-monospace, Consolas, monospace",
                color: "#0f172a",
                whiteSpace: "pre-wrap",
            }}
        >
            measuring…
        </div>
        <div style={{ display: "flex", gap: 24, padding: 24, fontFamily: "'Segoe UI', system-ui" }}>
            <Panel id="p-before" label="改前（100% = 原文宽度）" note="旧行为：mermaid 的自然尺寸被 max-width 钉死" />
            <Panel id="p-default" label="改后默认（125%）" note="不点任何按钮时的首屏大小" />
            <Panel id="p-in" label="点两次 +（175%）" note="放大后可横向滚动查看" />
            <Panel id="p-out" label="点两次 −（75%）" note="到 50% 时 − 会禁用" />
        </div>
    </>,
);

function clickTimes(panelId: string, testId: string, times: number) {
    const panel = document.getElementById(panelId);
    for (let i = 0; i < times; i += 1) {
        (panel?.querySelector(`[data-testid="${testId}"]`) as HTMLButtonElement | null)?.click();
    }
}

function drive() {
    if (document.querySelectorAll('[data-testid="assistant-mermaid-zoom-in"]').length < 4) {
        setTimeout(drive, 200);
        return;
    }
    // Reference panel at 100%: one step down from the new default.
    clickTimes("p-before", "assistant-mermaid-zoom-out", 1);
    clickTimes("p-in", "assistant-mermaid-zoom-in", 2);
    clickTimes("p-out", "assistant-mermaid-zoom-out", 2);
    // React flushes the click-driven state changes asynchronously, so read the
    // result on a later turn or the report captures the pre-click markup.
    setTimeout(() => {
        const labels = Array.from(document.querySelectorAll('[data-testid="assistant-mermaid-zoom-reset"]')).map((b) => b.textContent);
        const svgs = Array.from(document.querySelectorAll('[data-testid="assistant-mermaid-diagram"] svg'));
        const attrs = svgs.map((s) => s.getAttribute("width"));
        // What the browser actually laid out: the number that decides whether the
        // reader sees a bigger drawing.
        const rendered = svgs.map((s) => Math.round((s as SVGGraphicsElement).getBoundingClientRect().width));
        // client vs scroll width: equal means no horizontal overflow to pan.
        const scrollers = Array.from(
            document.querySelectorAll('[data-testid="assistant-mermaid-diagram"]'),
        ).map((d) => `${d.clientWidth}/${d.scrollWidth}`);
        const boxes = svgs.map((s) => `${s.getAttribute("viewBox")}|cs=${getComputedStyle(s).width}|attr=${s.getAttribute("width")}`);
        const first = svgs[0];
        const nodeRects = Array.from(first?.querySelectorAll("g.node rect") ?? []).slice(0, 2).map((r) =>
            `${Math.round((r as SVGGraphicsElement).getBBox().width)}x${Math.round((r as SVGGraphicsElement).getBBox().height)}`,
        );
        const diag = [
            `viewport=${window.innerWidth}x${window.innerHeight}`,
            `panelInner=${document.getElementById("p-before")?.clientWidth}`,
            `labels=${labels.join(",")}`,
            `attr=${attrs.join(",")}`,
            `rect=${rendered.join(",")}`,
            `client/scroll=${scrollers.join(",")}`,
            `viewBox=${first?.getAttribute("viewBox")}`,
            `nodeBoxes=${nodeRects.join(",")}`,
        ].join("   ");
        const diagEl = document.getElementById("diag");
        if (diagEl) diagEl.textContent = diag;
        document.title = diag;
    }, 600);
}

setTimeout(drive, 300);
