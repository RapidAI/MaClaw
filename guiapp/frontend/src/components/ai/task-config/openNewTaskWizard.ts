import { EVENT_OPEN_NEW_TASK_WIZARD } from "../../../constants/events";
import type { NewTaskWizardSeed } from "./taskDraft";

/** Open the new-task page. A seed pre-fills the expert, workflow, or LaTeX template. */
export function openNewTaskWizard(seed?: NewTaskWizardSeed): void {
    if (typeof window === "undefined") return;
    window.dispatchEvent(new CustomEvent(EVENT_OPEN_NEW_TASK_WIZARD, {
        detail: seed,
    }));
}
