import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { BugReportScreenshotMarkDataURL } from "../../../wailsjs/go/main/App";
import { BugReportShotEditor } from "./BugReportShotEditor";
import type { BugReportShotExporter } from "./bugReportAnnotate";

// Loads the full pasted picture (not the 240px list thumbnail) and mounts the
// same marker the header bug report uses. The parent keeps this mounted while
// the picture stays attached, and hides it when another picture is active, so
// strokes survive switching. A fresh remount would export nothing and the
// original file would replace the marks.
export function ProblemReportMarkEditor({
    lang,
    path,
    active,
    disabled,
    exporter,
    failedLabel,
    portalHost,
}: {
    lang: string;
    path: string;
    active: boolean;
    disabled: boolean;
    exporter: { current: BugReportShotExporter };
    failedLabel: string;
    // Portal target inside the report dialog. That dialog stays mounted while
    // it is closed; a new target remounts this marker and drops its strokes.
    portalHost: HTMLElement | null;
}) {
    const [src, setSrc] = useState("");
    const [failed, setFailed] = useState(false);
    useEffect(() => {
        let cancelled = false;
        setSrc("");
        setFailed(false);
        void BugReportScreenshotMarkDataURL(path).then(url => {
            if (cancelled) return;
            if (!url) {
                setFailed(true);
                return;
            }
            setSrc(url);
        }).catch(() => {
            if (!cancelled) setFailed(true);
        });
        return () => { cancelled = true; };
    }, [path]);
    let body;
    if (failed) {
        body = <div className="gui-bug-report-shot gui-bug-report-shot--pending" role="status">{failedLabel}</div>;
    } else if (!src) {
        body = <div className="gui-bug-report-shot gui-bug-report-shot--pending" role="status" aria-busy="true" />;
    } else {
        body = (
            <BugReportShotEditor
                lang={lang}
                src={src}
                active={active}
                disabled={disabled || !active}
                exporter={exporter}
                onPreviewError={() => setFailed(true)}
            />
        );
    }
    const marked = <div hidden={!active}>{body}</div>;
    if (portalHost) return createPortal(marked, portalHost);
    return <div hidden>{marked}</div>;
}
