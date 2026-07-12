import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import { KnowledgeImportJobStatus } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';
import {
    KnowledgeImportFloatingBar,
    isKnowledgeImportJobActive,
    isKnowledgeImportJobTerminal,
    mergeKnowledgeImportProgress,
    type ImportJob,
    type KnowledgeImportTFunc,
} from './KnowledgeImportDialog';

const EXPAND_FLAG = 'maclaw.knowledgeImport.expand';
export const KNOWLEDGE_IMPORT_EXPAND_EVENT = 'maclaw:knowledge-import-expand';

type KnowledgeImportContextValue = {
    job: ImportJob | null;
    dialogOpen: boolean;
    setDialogOpen: (open: boolean) => void;
    /** Publish job updates from the dialog (or other starters). */
    publishJob: (job: ImportJob | null) => void;
    /** Clear a finished job after the user dismisses the floating bar. */
    dismissTerminalJob: () => void;
    /** Navigate to Knowledge settings and reopen the import dialog. */
    requestExpand: () => void;
};

const KnowledgeImportContext = createContext<KnowledgeImportContextValue | null>(null);

export function useKnowledgeImport(): KnowledgeImportContextValue {
    const ctx = useContext(KnowledgeImportContext);
    if (!ctx) {
        throw new Error('useKnowledgeImport must be used within KnowledgeImportProvider');
    }
    return ctx;
}

/** Optional hook — returns null outside the provider (safe for tests). */
export function useKnowledgeImportOptional(): KnowledgeImportContextValue | null {
    return useContext(KnowledgeImportContext);
}

function resolveDocT(): KnowledgeImportTFunc {
    const raw = (typeof document !== 'undefined' ? document.documentElement.lang : '') || '';
    const lang = raw === 'zh-Hans' || raw === 'zh-CN' || raw === 'zh'
        ? 'zh-Hans'
        : raw === 'zh-Hant' || raw === 'zh-TW' || raw === 'zh-HK'
            ? 'zh-Hant'
            : 'en';
    return (en, zhHans, zhHant = zhHans) => {
        if (lang === 'zh-Hans') return zhHans;
        if (lang === 'zh-Hant') return zhHant;
        return en;
    };
}

export function KnowledgeImportProvider({ children }: { children: ReactNode }) {
    const [job, setJob] = useState<ImportJob | null>(null);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [dismissedJobId, setDismissedJobId] = useState('');
    const jobIdRef = useRef<string>('');
    jobIdRef.current = String(job?.id || '').trim();

    const publishJob = useCallback((next: ImportJob | null) => {
        setJob(next);
        if (next?.id && next.id === dismissedJobId) {
            setDismissedJobId('');
        }
    }, [dismissedJobId]);

    const dismissTerminalJob = useCallback(() => {
        setJob(prev => {
            if (prev?.id) setDismissedJobId(String(prev.id));
            return null;
        });
    }, []);

    const requestExpand = useCallback(() => {
        try {
            sessionStorage.setItem(EXPAND_FLAG, '1');
        } catch {
            /* ignore private mode */
        }
        // Reuse the existing settings-navigation bus; open Knowledge tab.
        window.dispatchEvent(new CustomEvent('maclaw:open-settings', { detail: { tab: 'knowledge' } }));
        window.dispatchEvent(new CustomEvent(KNOWLEDGE_IMPORT_EXPAND_EVENT));
    }, []);

    // Global progress bus — survives Knowledge settings unmount.
    useEffect(() => {
        const cleanup = EventsOn('knowledge:import-progress', (data: any) => {
            if (!data || typeof data !== 'object') return;
            const eventJobId = String(data.job_id || '').trim();
            if (!eventJobId) return;
            setJob(prev => {
                const prevId = String(prev?.id || '').trim();
                // Ignore events for a different job when one is already tracked,
                // unless we have no job yet (fresh start / remount recovery).
                if (prevId && prevId !== eventJobId) return prev;
                if (eventJobId === dismissedJobId && !isKnowledgeImportJobActive({ status: data.status })) {
                    return prev;
                }
                return mergeKnowledgeImportProgress(prev, data);
            });
        });
        return () => { cleanup(); };
    }, [dismissedJobId]);

    // Polling fallback while job is running (dialog may be unmounted).
    useEffect(() => {
        const id = String(job?.id || '').trim();
        if (!id || !isKnowledgeImportJobActive(job)) return;
        const handle = window.setInterval(() => {
            void KnowledgeImportJobStatus(id)
                .then(j => {
                    if (!j) return;
                    setJob(prev => {
                        if (String(prev?.id || '') !== id) return prev;
                        return j as ImportJob;
                    });
                })
                .catch(() => {});
        }, 2000);
        return () => window.clearInterval(handle);
    }, [job?.id, job?.status]);

    const showFloat = useMemo(() => {
        if (!job || dialogOpen) return false;
        if (String(job.id || '') === dismissedJobId) return false;
        return isKnowledgeImportJobActive(job) || isKnowledgeImportJobTerminal(job);
    }, [job, dialogOpen, dismissedJobId]);

    const t = useMemo(() => resolveDocT(), [job?.status, job?.result?.processed_files]);

    const value = useMemo<KnowledgeImportContextValue>(() => ({
        job,
        dialogOpen,
        setDialogOpen,
        publishJob,
        dismissTerminalJob,
        requestExpand,
    }), [job, dialogOpen, publishJob, dismissTerminalJob, requestExpand]);

    return (
        <KnowledgeImportContext.Provider value={value}>
            {children}
            {showFloat && job && (
                <KnowledgeImportFloatingBar
                    job={job}
                    t={t}
                    onExpand={requestExpand}
                    onDismiss={isKnowledgeImportJobTerminal(job) ? dismissTerminalJob : undefined}
                />
            )}
        </KnowledgeImportContext.Provider>
    );
}

/** Consume expand flag on mount / event (used by KnowledgeSettingsPanel). */
export function consumeKnowledgeImportExpandFlag(): boolean {
    try {
        if (sessionStorage.getItem(EXPAND_FLAG) === '1') {
            sessionStorage.removeItem(EXPAND_FLAG);
            return true;
        }
    } catch {
        /* ignore */
    }
    return false;
}
