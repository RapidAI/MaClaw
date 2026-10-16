import { useEffect, useState, type MutableRefObject } from "react";

export function useWorkflowStartingLabel(activateTabRef: MutableRefObject<(id: string) => void>) {
    const [workflowStartingLabel, setWorkflowStartingLabel] = useState<string | null>(null);

    // Uses sessionStorage as a cross-tab-switch channel because this panel
    // may not be mounted when a workflow tile is clicked.
    useEffect(() => {
        const consume = () => {
            const raw = sessionStorage.getItem("maclaw:workflow-starting");
            if (!raw) return;
            try {
                const data = JSON.parse(raw);
                if (data.ts && Date.now() - data.ts < 5000) {
                    setWorkflowStartingLabel(data.label || "...");
                    sessionStorage.removeItem("maclaw:workflow-starting");
                    if (data.activateLocal) {
                        activateTabRef.current("local");
                    }
                } else {
                    sessionStorage.removeItem("maclaw:workflow-starting");
                }
            } catch {
                sessionStorage.removeItem("maclaw:workflow-starting");
            }
        };
        consume();
        window.addEventListener("maclaw:workflow-starting-nudge", consume);
        return () => {
            window.removeEventListener("maclaw:workflow-starting-nudge", consume);
        };
    }, [activateTabRef]);

    useEffect(() => {
        if (!workflowStartingLabel) return;
        const timer = setTimeout(() => setWorkflowStartingLabel(null), 8000);
        return () => clearTimeout(timer);
    }, [workflowStartingLabel]);

    return { workflowStartingLabel, setWorkflowStartingLabel };
}
