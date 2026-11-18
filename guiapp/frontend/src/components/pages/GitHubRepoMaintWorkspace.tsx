import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { tailText } from '../../i18n/mnTail';

type MaintRule = {
    id?: string;
    repo_url?: string;
    owner?: string;
    repo?: string;
    ghost_dir?: string;
    enabled?: boolean;
    push_branch?: string;
    commands?: Record<string, string>;
    enabled_kinds?: Record<string, boolean>;
    hook_ready?: boolean;
    auth_blocked?: boolean;
    last_error?: string;
    queue_depth?: number;
    intake_ready?: boolean;
};

type GitHubConnection = {
    connected?: boolean;
    login?: string;
    scopes?: string;
    custom_client_id?: boolean;
    user_code?: string;
    verification_uri?: string;
};

type MaintApp = {
    GetGitHubConnection?: (refresh: boolean) => Promise<GitHubConnection>;
    StartGitHubOAuth?: () => Promise<GitHubConnection>;
    WaitGitHubOAuth?: () => Promise<GitHubConnection>;
    CancelGitHubOAuth?: () => Promise<unknown>;
    LoadConfigForUI?: () => Promise<Record<string, string>>;
    ListGitHubRepoMaintRules?: () => Promise<MaintRule[]>;
    SaveGitHubRepoMaintRule?: (rule: MaintRule) => Promise<MaintRule>;
    DeleteGitHubRepoMaintRule?: (id: string) => Promise<void>;
    SetGitHubRepoMaintEnabled?: (id: string, enabled: boolean, replay: string) => Promise<void>;
    ReplayGitHubRepoMaint?: (id: string) => Promise<void>;
    GitHubAPIRequest?: (method: string, apiPath: string) => Promise<string>;
    SelectWorkingDir?: () => Promise<string>;
};

const EVENTS: Array<{ kind: string; en: string; hans: string; hant: string; issue: boolean }> = [
    { kind: 'issues.opened', en: 'Issue opened', hans: '议题已创建', hant: '議題已建立', issue: true },
    { kind: 'issues.closed', en: 'Issue closed', hans: '议题已关闭', hant: '議題已關閉', issue: true },
    { kind: 'issues.labeled', en: 'Issue labeled', hans: '议题已添加标签', hant: '議題已新增標籤', issue: true },
    { kind: 'issue_comment.created', en: 'New comment', hans: '新评论', hant: '新評論', issue: true },
    { kind: 'pull_request.opened', en: 'Pull request opened', hans: '拉取请求已创建', hant: '拉取請求已建立', issue: false },
    { kind: 'pull_request.merged', en: 'Pull request merged', hans: '已合并', hant: '已合併', issue: false },
    { kind: 'pull_request.review_requested', en: 'Review requested', hans: '已请求审查', hant: '已請求審查', issue: false },
    { kind: 'pull_request_review.submitted', en: 'Review submitted', hans: '已提交审查', hant: '已提交審查', issue: false },
    { kind: 'push', en: 'Push', hans: '新提交', hant: '新提交', issue: false },
    { kind: 'check_run.failure', en: 'Check failed', hans: '检查失败', hant: '檢查失敗', issue: false },
];

const ISSUE_EVENTS = EVENTS.filter((event) => event.issue);
const CODE_EVENTS = EVENTS.filter((event) => !event.issue);

function maintApp(): MaintApp | null {
    const app = (window as unknown as { go?: { main?: { App?: MaintApp } } }).go?.main?.App;
    return app ?? null;
}

function text(lang: string, en: string, hans: string, hant: string): string {
    return tailText(lang, en, hans, hant);
}

function scopesHaveRepo(scopes?: string): boolean {
    return String(scopes || '').split(/[\s,]+/).some((part) => part === 'repo');
}

function blankDraft(): MaintRule {
    return {
        repo_url: '',
        ghost_dir: '',
        push_branch: '',
        commands: {},
        enabled_kinds: {},
        enabled: false,
        hook_ready: false,
        intake_ready: true,
    };
}

function issueKindsArmed(rule: MaintRule): boolean {
    const kinds = rule.enabled_kinds || {};
    return EVENTS.some((event) => event.issue && kinds[event.kind]);
}

const IconGitBranch = () => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <line x1="6" y1="3" x2="6" y2="15" />
        <circle cx="18" cy="6" r="3" />
        <circle cx="6" cy="18" r="3" />
        <path d="M18 9a9 9 0 0 1-9 9" />
    </svg>
);

const IconPlug = () => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M9 7V3M15 7V3" />
        <path d="M7 7h10v4a5 5 0 0 1-5 5 5 5 0 0 1-5-5z" />
        <path d="M12 16v5" />
    </svg>
);

const IconWarn = () => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.9} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M12 3.5 21 19H3z" />
        <path d="M12 10v4M12 16.6v.01" />
    </svg>
);

// `.app-scale-layer` scales with transform. A fixed descendant is then fixed
// to that layer, and `.utilities-page` clips it, so a tall editor loses its
// save row. The viewport is outside that transform and already carries the
// theme attributes, the same host the expert editor uses.
function placeMaintDialogs(node: ReactNode): ReactNode {
    if (typeof document === 'undefined') return node;
    const host = document.querySelector<HTMLElement>('.app-viewport') || document.body;
    return createPortal(node, host);
}

function maintDialogFocusable(dialog: HTMLElement): HTMLElement[] {
    const selector = [
        'a[href]',
        'button:not([disabled])',
        'input:not([disabled])',
        'select:not([disabled])',
        'textarea:not([disabled])',
        '[tabindex]:not([tabindex="-1"])',
    ].join(',');
    return Array.from(dialog.querySelectorAll<HTMLElement>(selector)).filter((element) => (
        !element.hasAttribute('disabled')
        && element.getAttribute('aria-hidden') !== 'true'
        && element.tabIndex >= 0
    ));
}

export function GitHubRepoMaintWorkspace({ lang, onBack }: { lang: string; onBack: () => void }) {
    const [connection, setConnection] = useState<GitHubConnection>({ connected: false });
    const [device, setDevice] = useState<{ code: string; uri: string } | null>(null);
    const [hubReady, setHubReady] = useState(false);
    const [rules, setRules] = useState<MaintRule[]>([]);
    const [draft, setDraft] = useState<MaintRule>(blankDraft());
    // The editor is a dialog. Leaving it closed keeps the list as the page,
    // so saving one rule does not occupy the only form on the screen.
    const [editorOpen, setEditorOpen] = useState(false);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    // setBusy only disables the button on the next paint. A second click in
    // that gap inserts another row, because a new rule has no id yet.
    const actionLockRef = useRef(false);
    const [gate, setGate] = useState<{ rule: MaintRule; step: 'public' | 'choice' } | null>(null);
    // Rule awaiting the delete confirmation; the destructive call runs only
    // through that dialog, mirroring the enable gates.
    const [deleteTarget, setDeleteTarget] = useState<MaintRule | null>(null);
    // Which event the user just armed (so its command editor can take focus)
    // and which one the save validator rejected (red highlight at the row).
    const [focusKind, setFocusKind] = useState('');
    const [invalidKind, setInvalidKind] = useState('');
    const [codeCopied, setCodeCopied] = useState(false);
    // Tracks the "Copied" feedback reset so a repeated click cannot stack timers
    // and unmount cannot fire it into a removed panel.
    const copyResetTimerRef = useRef(0);
    // Set when the user cancels the device login so the resulting "已取消" wait
    // error stays silent instead of surfacing as a failure (backend keeps the
    // cancellation quiet for the same reason).
    const oauthCancelledRef = useRef(false);
    // The control that opened the dialog. The portal moves focus into the
    // dialog before effects run, so this is captured in the click handler.
    const restoreFocusRef = useRef<HTMLElement | null>(null);
    const workspaceRef = useRef<HTMLDivElement>(null);

    const rememberFocus = () => {
        const active = document.activeElement;
        if (!(active instanceof HTMLElement)) return;
        if (active.closest('[data-maint-dialog]')) return;
        restoreFocusRef.current = active;
    };

    useEffect(() => () => window.clearTimeout(copyResetTimerRef.current), []);

    const reload = useCallback(async () => {
        const app = maintApp();
        if (!app) return;
        if (app.GetGitHubConnection) {
            setConnection(await app.GetGitHubConnection(false));
        }
        if (app.LoadConfigForUI) {
            const cfg = await app.LoadConfigForUI();
            const url = String(cfg?.remote_hub_url || '').trim();
            const id = String(cfg?.remote_machine_id || '').trim();
            const token = String(cfg?.remote_machine_token || '').trim();
            setHubReady(url !== '' && id !== '' && token !== '');
        }
        if (app.ListGitHubRepoMaintRules) {
            const listed = await app.ListGitHubRepoMaintRules();
            setRules(Array.isArray(listed) ? listed : []);
        }
    }, []);

    useEffect(() => {
        let cancelled = false;
        void (async () => {
            try {
                await reload();
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [reload]);

    const closeEditor = () => {
        setEditorOpen(false);
        setDraft(blankDraft());
        setInvalidKind('');
        setFocusKind('');
        // The message belongs to the draft that just closed. Leaving it on
        // the list shows a command error for a form that is no longer there.
        setError('');
    };

    const openEditor = (rule?: MaintRule) => {
        rememberFocus();
        setError('');
        setInvalidKind('');
        if (rule) {
            setDraft({
                ...blankDraft(),
                ...rule,
                commands: { ...(rule.commands || {}) },
                enabled_kinds: { ...(rule.enabled_kinds || {}) },
            });
            // Repository and directory are locked while the rule is on, so
            // the caret starts on the command the user can still change.
            const armed = rule.enabled
                ? EVENTS.find((event) => rule.enabled_kinds?.[event.kind])?.kind || ''
                : '';
            setFocusKind(armed);
        } else {
            setDraft(blankDraft());
            setFocusKind('');
        }
        setEditorOpen(true);
    };

    // Escape closes the top dialog. Enable and delete confirmations sit above
    // the editor, so they go first and leave the draft alone. A request in
    // flight keeps the dialog: Cancel is already disabled, and closing here
    // would make Escape look like it aborted a save or delete that still runs.
    const overlayOpen = gate !== null || deleteTarget !== null || editorOpen;
    useEffect(() => {
        if (!overlayOpen) return;
        const onKey = (event: KeyboardEvent) => {
            if ((event as KeyboardEvent & { isComposing?: boolean }).isComposing) return;
            if (event.key === 'Escape') {
                event.preventDefault();
                event.stopPropagation();
                // The lock flips in the click, before busy repaints. Escape
                // must see that, or it closes a save that is already running.
                if (actionLockRef.current) return;
                if (gate || deleteTarget) {
                    setGate(null);
                    setDeleteTarget(null);
                    return;
                }
                closeEditor();
                return;
            }
            if (event.key !== 'Tab') return;
            const dialogs = document.querySelectorAll<HTMLElement>('[data-maint-dialog="true"]');
            const dialog = dialogs[dialogs.length - 1];
            if (!dialog) return;
            const focusable = maintDialogFocusable(dialog);
            if (focusable.length === 0) {
                event.preventDefault();
                return;
            }
            const active = document.activeElement;
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            const inside = active instanceof Node && dialog.contains(active);
            if (event.shiftKey && (!inside || active === first)) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && (!inside || active === last)) {
                event.preventDefault();
                first.focus();
            }
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [overlayOpen, gate, deleteTarget]);

    // The portaled dialog is outside the page. inert keeps the list from
    // taking clicks and Tab. Focus goes back to the opener only after inert
    // is cleared, or the opener would still be unfocusable.
    useEffect(() => {
        const page = workspaceRef.current;
        if (overlayOpen) page?.setAttribute('inert', '');
        else page?.removeAttribute('inert');
        if (!overlayOpen) return;
        return () => {
            page?.removeAttribute('inert');
            const back = restoreFocusRef.current;
            restoreFocusRef.current = null;
            if (back?.isConnected) back.focus();
        };
    }, [overlayOpen]);

    // The save validator highlights the offending event inside the scrolling
    // list. Scroll that list only; scrollIntoView would also move the overlay
    // and slide the save row off the frame.
    useEffect(() => {
        if (!invalidKind) return;
        const field = document.querySelector<HTMLTextAreaElement>(`[data-testid="github-repo-maint-command-${invalidKind}"]`);
        if (!field) return;
        field.focus({ preventScroll: true });
        const scroller = field.closest('.utilities-repomaint-editor-scroll');
        if (!(scroller instanceof HTMLElement)) return;
        const fieldRect = field.getBoundingClientRect();
        const scrollerRect = scroller.getBoundingClientRect();
        const above = scrollerRect.top - fieldRect.top;
        const below = fieldRect.bottom - scrollerRect.bottom;
        if (above > 0) scroller.scrollTop -= above + 8;
        else if (below > 0) scroller.scrollTop += below + 8;
    }, [invalidKind]);

    const runBusy = useCallback(async (fn: () => Promise<void>) => {
        if (actionLockRef.current) return false;
        actionLockRef.current = true;
        setBusy(true);
        setError('');
        try {
            await fn();
            return true;
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
            return false;
        } finally {
            actionLockRef.current = false;
            setBusy(false);
        }
    }, []);

    const connect = async () => {
        const app = maintApp();
        const startOAuth = app?.StartGitHubOAuth;
        const waitOAuth = app?.WaitGitHubOAuth;
        if (!startOAuth || !waitOAuth) {
            setError(text(lang, 'GitHub login is unavailable', 'GitHub 登录不可用', 'GitHub 登入不可用'));
            return;
        }
        oauthCancelledRef.current = false;
        await runBusy(async () => {
            const info = await startOAuth();
            const code = String(info?.user_code || '');
            // An empty code means the backend could not start the device flow;
            // showing an empty panel would just confuse.
            if (code) setDevice({ code, uri: String(info?.verification_uri || '') });
            let status: GitHubConnection | undefined;
            try {
                status = await waitOAuth();
            } catch (waitError) {
                // A failed wait must not leave a stale "waiting for authorization"
                // panel behind; a login the user just cancelled stays silent.
                setDevice(null);
                if (!oauthCancelledRef.current) throw waitError;
                return;
            }
            setConnection(status || { connected: false });
            setDevice(null);
        });
    };

    const cancelConnect = async () => {
        oauthCancelledRef.current = true;
        setDevice(null);
        try {
            await maintApp()?.CancelGitHubOAuth?.();
        } catch { /* Best effort: the wait also settles on its own timeout. */ }
    };

    const pickDir = async () => {
        const app = maintApp();
        if (!app?.SelectWorkingDir) return;
        const dir = await app.SelectWorkingDir();
        if (dir) setDraft((prev) => ({ ...prev, ghost_dir: dir }));
    };

    const copyDeviceCode = async () => {
        if (!device?.code) return;
        try {
            await navigator.clipboard.writeText(device.code);
            setCodeCopied(true);
            window.clearTimeout(copyResetTimerRef.current);
            copyResetTimerRef.current = window.setTimeout(() => setCodeCopied(false), 1600);
        } catch { /* Clipboard access can be unavailable; the code stays selectable. */ }
    };

    const save = async () => {
        if (actionLockRef.current) return;
        const kinds = draft.enabled_kinds || {};
        const commands = draft.commands || {};
        setInvalidKind('');
        for (const event of EVENTS) {
            if (kinds[event.kind] && !String(commands[event.kind] || '').trim()) {
                setInvalidKind(event.kind);
                setError(text(lang, 'An enabled event needs a command', '打开的事件必须填写命令', '打開的事件必須填寫命令'));
                return;
            }
        }
        const saveRule = maintApp()?.SaveGitHubRepoMaintRule;
        if (!saveRule) return;
        await runBusy(async () => {
            await saveRule({
                id: draft.id,
                // Trailing whitespace in a path or URL is always a typing slip;
                // normalize before persisting.
                repo_url: String(draft.repo_url || '').trim(),
                ghost_dir: String(draft.ghost_dir || '').trim(),
                push_branch: draft.push_branch,
                commands,
                enabled_kinds: kinds,
            });
            // A saved rule goes back to the list. Keeping its id in the form
            // made the next save overwrite that same row.
            closeEditor();
            await reload();
        });
    };

    const commitEnable = async (rule: MaintRule, replay: string) => {
        const ruleId = rule.id;
        const setEnabled = maintApp()?.SetGitHubRepoMaintEnabled;
        if (!ruleId || !setEnabled) return;
        setGate(null);
        await runBusy(async () => {
            await setEnabled(ruleId, true, replay);
            await reload();
        });
    };

    const requestEnable = async (rule: MaintRule) => {
        if (!connection.connected) {
            setError(text(lang, 'Connect GitHub first', '请先连接 GitHub', '請先連接 GitHub'));
            return;
        }
        if (!scopesHaveRepo(connection.scopes)) {
            setError(text(lang, 'Reconnect GitHub so the token includes repo', '请重新连接 GitHub，令牌需要包含 repo', '請重新連接 GitHub，權杖需要包含 repo'));
            return;
        }
        if (!hubReady) {
            setError(text(lang, 'Connect this desktop to Hub before enabling', '启用前要先连上 Hub', '啟用前要先連上 Hub'));
            return;
        }
        // Disabling the switch drops focus. Capture it before that, and do
        // not capture again after the privacy await: focus has already
        // moved, and closing the gate must return to the switch.
        let focusHeld = false;
        const holdFocus = () => {
            if (focusHeld) return;
            rememberFocus();
            focusHeld = true;
        };
        let needsPublic = false;
        if (issueKindsArmed(rule)) {
            needsPublic = true;
            const app = maintApp();
            if (app?.GitHubAPIRequest && rule.owner && rule.repo) {
                // Hold the same action lock as save. The privacy request is
                // an await, and a second click in that gap used to start
                // another enable or a disable before this one decided.
                if (actionLockRef.current) return;
                holdFocus();
                actionLockRef.current = true;
                setBusy(true);
                setError('');
                try {
                    const raw = await app.GitHubAPIRequest('GET', `/repos/${rule.owner}/${rule.repo}`);
                    const doc = JSON.parse(raw) as { private?: boolean };
                    needsPublic = doc.private !== true;
                } catch {
                    needsPublic = true;
                } finally {
                    actionLockRef.current = false;
                    setBusy(false);
                }
            }
        }
        if (actionLockRef.current) return;
        if (needsPublic) {
            holdFocus();
            setGate({ rule, step: 'public' });
            return;
        }
        if (!rule.hook_ready) {
            holdFocus();
            setGate({ rule, step: 'choice' });
            return;
        }
        await commitEnable(rule, '');
    };

    const toggle = async (rule: MaintRule, next: boolean) => {
        const ruleId = rule.id;
        const setEnabled = maintApp()?.SetGitHubRepoMaintEnabled;
        if (!ruleId || !setEnabled) return;
        if (!next) {
            await runBusy(async () => {
                await setEnabled(ruleId, false, '');
                await reload();
            });
            return;
        }
        await requestEnable(rule);
    };

    const replay = async (rule: MaintRule) => {
        const ruleId = rule.id;
        const replayOpen = maintApp()?.ReplayGitHubRepoMaint;
        if (!ruleId || !replayOpen) return;
        await runBusy(async () => {
            await replayOpen(ruleId);
            await reload();
        });
    };

    const confirmDelete = async () => {
        const ruleId = deleteTarget?.id;
        const deleteRule = maintApp()?.DeleteGitHubRepoMaintRule;
        if (!ruleId || !deleteRule) {
            setDeleteTarget(null);
            return;
        }
        // A second click while the first request is still out must not treat
        // the skipped call as a finished delete and close the dialog early.
        if (actionLockRef.current) return;
        // A "delete pending" failure (Hub or hook unreachable) closes the dialog
        // and surfaces the retry message through the page-level alert.
        const deleted = await runBusy(() => deleteRule(ruleId));
        setDeleteTarget(null);
        if (deleted) {
            if (draft.id === ruleId) closeEditor();
            await reload();
        }
    };

    const intakeReady = rules.length === 0 ? draft.intake_ready !== false : rules.every((rule) => rule.intake_ready !== false);
    const draftArmedCount = EVENTS.filter((event) => draft.enabled_kinds?.[event.kind]).length;
    const armedIssueCount = ISSUE_EVENTS.filter((event) => draft.enabled_kinds?.[event.kind]).length;
    const armedCodeCount = CODE_EVENTS.filter((event) => draft.enabled_kinds?.[event.kind]).length;
    const scopeMissing = connection.connected && !scopesHaveRepo(connection.scopes);
    // An enabled rule's webhook and checkout are already bound. The save
    // rejects a different repository or directory until the switch is off.
    const identityLocked = !!draft.id && !!draft.enabled;

    const renderEvent = (event: (typeof EVENTS)[number]) => {
        const on = !!draft.enabled_kinds?.[event.kind];
        const invalid = invalidKind === event.kind;
        return (
            <div
                key={event.kind}
                className={`utilities-repomaint-event${on ? ' utilities-repomaint-event--on' : ''}${invalid ? ' utilities-repomaint-event--invalid' : ''}`}
            >
                <label className="utilities-repomaint-event__head">
                    <input
                        type="checkbox"
                        data-testid={`github-repo-maint-kind-${event.kind}`}
                        checked={on}
                        autoFocus={identityLocked && focusKind === '' && event.kind === EVENTS[0].kind}
                        onChange={(change) => {
                            if (change.target.checked) setFocusKind(event.kind);
                            else if (invalid) {
                                setInvalidKind('');
                                setError('');
                            }
                            setDraft({
                                ...draft,
                                enabled_kinds: { ...(draft.enabled_kinds || {}), [event.kind]: change.target.checked },
                            });
                        }}
                    />
                    <span className="utilities-repomaint-event__name">{text(lang, event.en, event.hans, event.hant)}</span>
                    <code className="utilities-repomaint-event__kind">{event.kind}</code>
                </label>
                {on ? (
                    <div className="utilities-repomaint-event__body">
                        <label className="utilities-repomaint-command-field">
                            <span>{text(lang, 'Command', '命令', '命令')}</span>
                            <textarea
                                className="utilities-input utilities-repomaint-command"
                                data-testid={`github-repo-maint-command-${event.kind}`}
                                rows={2}
                                placeholder={text(lang, 'Command to run when this event arrives', '事件到达时执行的命令', '事件到達時執行的命令')}
                                value={draft.commands?.[event.kind] || ''}
                                autoFocus={focusKind === event.kind}
                                onChange={(change) => {
                                    if (invalid) {
                                        setInvalidKind('');
                                        setError('');
                                    }
                                    setDraft({
                                        ...draft,
                                        commands: { ...(draft.commands || {}), [event.kind]: change.target.value },
                                    });
                                }}
                            />
                        </label>
                        {event.kind === 'push' ? (
                            <label className="utilities-repomaint-command-field">
                                <span>{text(lang, 'Branch filter', '分支过滤', '分支過濾')}</span>
                                <input
                                    className="utilities-input"
                                    data-testid="github-repo-maint-push-branch"
                                    placeholder={text(lang, 'Empty uses the default branch', '留空则使用默认分支', '留空則使用預設分支')}
                                    value={draft.push_branch || ''}
                                    onChange={(change) => setDraft({ ...draft, push_branch: change.target.value })}
                                />
                            </label>
                        ) : null}
                    </div>
                ) : null}
            </div>
        );
    };

    return (
        <div ref={workspaceRef} className="utilities-page utilities-deploy" data-testid="github-repo-maint-workspace">
            <div className="utilities-page__header utilities-deploy__header">
                <button type="button" className="utilities-deploy__back" data-testid="github-repo-maint-back" onClick={onBack}>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M14 6l-6 6 6 6" /></svg>
                    {text(lang, 'Back', '返回', '返回')}
                </button>
                <div className="utilities-deploy__hero">
                    <span className="utilities-deploy__hero-icon" aria-hidden="true"><IconGitBranch /></span>
                    <div className="utilities-deploy__hero-text">
                        <h2 className="utilities-page__title">{text(lang, 'GitHub repository maintenance', 'GitHub 仓库自动维护', 'GitHub 倉庫自動維護')}</h2>
                        <p className="utilities-page__subtitle">{text(lang, 'One rule maintains one repository from a local checkout', '一条规则维护一个仓库的本地检出', '一條規則維護一個倉庫的本機檢出')}</p>
                    </div>
                </div>
            </div>
            {error && !editorOpen ? <p className="utilities-error" role="alert" data-testid="github-repo-maint-error">{error}</p> : null}
            {intakeReady ? null : <p className="utilities-hint utilities-hint--warn" data-testid="github-repo-maint-intake-off">{text(lang, 'Intake is not ready', '接收尚未就绪', '接收尚未就緒')}</p>}
            <section className="utilities-deploy-card" data-testid="github-repo-maint-connection">
                <div className="utilities-deploy-card__head">
                    <span className="utilities-deploy-card__head-icon" aria-hidden="true"><IconPlug /></span>
                    <h3 className="utilities-deploy-card__title">{text(lang, 'GitHub connection', 'GitHub 连接', 'GitHub 連接')}</h3>
                    <span className="utilities-repomaint-status" data-status={connection.connected ? 'ready' : 'off'}>
                        <span className="utilities-repomaint-status__dot" aria-hidden="true" />
                        {connection.connected
                            ? text(lang, 'Connected', '已连接', '已連接')
                            : text(lang, 'Not connected', '未连接', '未連接')}
                    </span>
                </div>
                {connection.connected ? (
                    <dl className="utilities-deploy-record__fields">
                        <div className="utilities-deploy-record__field">
                            <dt>{text(lang, 'Account', '账号', '帳號')}</dt>
                            <dd><code>{connection.login ? `@${connection.login}` : text(lang, 'Unnamed account', '未知账号', '未知帳號')}</code></dd>
                        </div>
                        {scopeMissing ? (
                            <div className="utilities-deploy-record__field">
                                <dt>{text(lang, 'Token scope', '令牌权限', '權杖權限')}</dt>
                                <dd>{text(lang, 'Missing the repo scope — reconnect to add it', '缺少 repo 权限，重新连接即可补上', '缺少 repo 權限，重新連接即可補上')}</dd>
                            </div>
                        ) : null}
                    </dl>
                ) : (
                    <p className="utilities-deploy-note">{text(lang, 'Sign in with a GitHub device code to let this desktop watch repository events for you.', '使用 GitHub 设备码登录，让本机替你盯守仓库事件。', '使用 GitHub 裝置碼登入，讓本機替你盯守倉庫事件。')}</p>
                )}
                {device ? (
                    <div className="utilities-repomaint-device" data-testid="github-repo-maint-user-code">
                        <span className="utilities-repomaint-device__label">{device.uri
                            ? text(lang, `Enter this code at ${device.uri}`, `在 ${device.uri} 输入此码`, `在 ${device.uri} 輸入此碼`)
                            : text(lang, 'Enter this code at github.com/login/device', '在 github.com/login/device 输入此码', '在 github.com/login/device 輸入此碼')}</span>
                        <span className="utilities-repomaint-device__coderow">
                            <code className="utilities-repomaint-device__code">{device.code}</code>
                            <button
                                type="button"
                                className="utilities-btn utilities-repomaint-device__copy"
                                data-testid="github-repo-maint-copy-code"
                                onClick={() => { void copyDeviceCode(); }}
                            >
                                {codeCopied
                                    ? text(lang, 'Copied', '已复制', '已複製')
                                    : text(lang, 'Copy', '复制', '複製')}
                            </button>
                        </span>
                        {connection.custom_client_id ? null : (
                            <span className="utilities-repomaint-device__label">{text(lang, 'The built-in login is shown as GitHub CLI. Approve repository access on that page.', '内置登录的授权页名称是 GitHub CLI。请在该页批准仓库权限。', '內建登入的授權頁名稱是 GitHub CLI。請在該頁批准倉庫權限。')}</span>
                        )}
                        <span className="utilities-repomaint-device__waitrow">
                            <span className="utilities-repomaint-device__wait">{text(lang, 'Waiting for authorization…', '等待授权中…', '等待授權中…')}</span>
                            <button
                                type="button"
                                className="utilities-btn utilities-repomaint-device__cancel"
                                data-testid="github-repo-maint-cancel-connect"
                                onClick={() => { void cancelConnect(); }}
                            >
                                {text(lang, 'Cancel', '取消', '取消')}
                            </button>
                        </span>
                    </div>
                ) : null}
                {!hubReady ? <p className="utilities-hint utilities-hint--warn" data-testid="github-repo-maint-hub-hint">{text(lang, 'Connect this desktop to Hub before enabling', '启用前要先连上 Hub', '啟用前要先連上 Hub')}</p> : null}
                {!connection.connected || scopeMissing ? (
                    <div className="utilities-deploy-card__actions">
                        <button
                            type="button"
                            className="utilities-btn utilities-btn--primary utilities-deploy-card__submit"
                            data-testid="github-repo-maint-connect"
                            disabled={busy}
                            aria-busy={busy || undefined}
                            onClick={() => { void connect(); }}
                        >
                            {scopeMissing
                                ? text(lang, 'Reconnect GitHub', '重新连接 GitHub', '重新連接 GitHub')
                                : text(lang, 'Connect GitHub', '连接 GitHub', '連接 GitHub')}
                        </button>
                    </div>
                ) : null}
            </section>
            <section className="utilities-deploy-records">
                <div className="utilities-deploy-records__head">
                    <h3 className="utilities-deploy-records__title">{text(lang, 'Maintenance rules', '维护规则', '維護規則')}</h3>
                    {rules.length ? <span className="utilities-deploy-records__count">{rules.length}</span> : null}
                    <span className="utilities-repomaint-rule-spacer" />
                    <button
                        type="button"
                        className="utilities-btn utilities-btn--primary"
                        data-testid="github-repo-maint-new"
                        disabled={busy || rules.length >= 20}
                        onClick={() => openEditor()}
                    >
                        {text(lang, 'New rule', '新建规则', '新建規則')}
                    </button>
                </div>
                {rules.length >= 20 ? (
                    <p className="utilities-hint" data-testid="github-repo-maint-cap">{text(lang, '20 rules is the maximum', '最多 20 条规则', '最多 20 條規則')}</p>
                ) : null}
                {rules.length === 0 ? (
                    <div className="utilities-deploy-empty">
                        <span className="utilities-deploy-empty__icon" aria-hidden="true"><IconGitBranch /></span>
                        <p>{text(lang, 'No rules yet. Each rule keeps one repository. Use New rule to add the first.', '还没有规则。一条规则维护一个仓库，点「新建规则」添加。', '還沒有規則。一條規則維護一個倉庫，點「新建規則」添加。')}</p>
                    </div>
                ) : (
                    <ul className="utilities-deploy-records__list" data-testid="github-repo-maint-rules">
                        {rules.map((rule) => {
                            const ruleHasError = !!rule.last_error || !!rule.auth_blocked;
                            const armedCount = EVENTS.filter((event) => rule.enabled_kinds?.[event.kind]).length;
                            return (
                                <li
                                    key={rule.id}
                                    className="utilities-deploy-record utilities-repomaint-card"
                                    data-status={ruleHasError ? 'failed' : rule.enabled ? 'ready' : 'off'}
                                    data-testid={`github-repo-maint-rule-${rule.id}`}
                                >
                                    <div className="utilities-deploy-record__head">
                                        <div className="utilities-deploy-record__target">
                                            {rule.owner && rule.repo ? (
                                                <>
                                                    <span className="utilities-deploy-record__name utilities-deploy-record__name--mono">{rule.owner}/{rule.repo}</span>
                                                    {rule.repo_url ? <span className="utilities-deploy-record__addr">{rule.repo_url}</span> : null}
                                                </>
                                            ) : (
                                                <span className="utilities-deploy-record__name utilities-deploy-record__name--mono">{rule.repo_url || rule.id}</span>
                                            )}
                                        </div>
                                        <div className="utilities-repomaint-rule-badges">
                                            {(rule.queue_depth || 0) > 0 ? (
                                                <span className="utilities-deploy-record__badge" data-status="pending" data-testid={`github-repo-maint-queue-${rule.id}`}>
                                                    <span className="utilities-deploy-record__badge-dot" aria-hidden="true" />
                                                    {text(lang, `Queued: ${rule.queue_depth || 0}`, `排队 ${rule.queue_depth || 0}`, `排隊 ${rule.queue_depth || 0}`)}
                                                </span>
                                            ) : null}
                                            {armedCount > 0 ? (
                                                <span className="utilities-deploy-record__chip">
                                                    {text(lang, `${armedCount} events armed`, `已启用 ${armedCount} 类事件`, `已啟用 ${armedCount} 類事件`)}
                                                </span>
                                            ) : null}
                                            {rule.auth_blocked ? (
                                                <span className="utilities-deploy-record__badge" data-status="failed">
                                                    <span className="utilities-deploy-record__badge-dot" aria-hidden="true" />
                                                    {text(lang, 'GitHub token was revoked', 'GitHub 令牌已失效', 'GitHub 權杖已失效')}
                                                </span>
                                            ) : rule.last_error ? (
                                                <span className="utilities-deploy-record__badge" data-status="failed">
                                                    <span className="utilities-deploy-record__badge-dot" aria-hidden="true" />
                                                    {text(lang, 'Last run failed', '上次运行失败', '上次執行失敗')}
                                                </span>
                                            ) : (
                                                <span className="utilities-deploy-record__badge" data-status={rule.enabled ? 'ready' : 'off'}>
                                                    <span className="utilities-deploy-record__badge-dot" aria-hidden="true" />
                                                    {rule.enabled ? text(lang, 'Enabled', '已启用', '已啟用') : text(lang, 'Disabled', '已停用', '已停用')}
                                                </span>
                                            )}
                                        </div>
                                    </div>
                                    <dl className="utilities-deploy-record__fields utilities-repomaint-fields">
                                        <div className="utilities-deploy-record__field">
                                            <dt>{text(lang, 'Checkout', '本地目录', '本機目錄')}</dt>
                                            <dd><code>{rule.ghost_dir || '—'}</code></dd>
                                        </div>
                                        <div className="utilities-deploy-record__field">
                                            <dt>{text(lang, 'Webhook', 'Webhook', 'Webhook')}</dt>
                                            <dd>{rule.hook_ready ? text(lang, 'Installed', '已挂上', '已掛上') : text(lang, 'Not installed', '尚未挂上', '尚未掛上')}</dd>
                                        </div>
                                        {rule.push_branch ? (
                                            <div className="utilities-deploy-record__field">
                                                <dt>{text(lang, 'Push branch', '推送分支', '推送分支')}</dt>
                                                <dd><code>{rule.push_branch}</code></dd>
                                            </div>
                                        ) : null}
                                    </dl>
                                    {rule.last_error ? (
                                        <p className="utilities-deploy-record__message" data-testid={`github-repo-maint-error-${rule.id}`}>{rule.last_error}</p>
                                    ) : null}
                                    <div className="utilities-deploy-record__footer utilities-repomaint-rule-actions">
                                        {rule.intake_ready !== false ? (
                                            <>
                                                <label className="utilities-repomaint-switch">
                                                    <input
                                                        type="checkbox"
                                                        data-testid={`github-repo-maint-enable-${rule.id}`}
                                                        checked={!!rule.enabled}
                                                        disabled={busy}
                                                        onChange={(event) => { void toggle(rule, event.target.checked); }}
                                                    />
                                                    <span className="utilities-repomaint-switch__track" aria-hidden="true"><span className="utilities-repomaint-switch__thumb" /></span>
                                                    <span className="utilities-repomaint-switch__label">{text(lang, 'Enabled', '总开关', '總開關')}</span>
                                                </label>
                                                <button
                                                    type="button"
                                                    className="utilities-btn utilities-repomaint-replay"
                                                    data-testid={`github-repo-maint-replay-${rule.id}`}
                                                    disabled={busy}
                                                    onClick={() => { void replay(rule); }}
                                                >
                                                    {text(lang, 'Replay open items', '回放未处理的', '回放未處理的')}
                                                </button>
                                            </>
                                        ) : null}
                                        <span className="utilities-repomaint-rule-spacer" />
                                        <button
                                            type="button"
                                            className="utilities-btn utilities-btn--quiet-danger utilities-repomaint-delete"
                                            data-testid={`github-repo-maint-delete-${rule.id}`}
                                            disabled={busy}
                                            onClick={() => { rememberFocus(); setDeleteTarget(rule); }}
                                        >
                                            {text(lang, 'Delete', '删除', '刪除')}
                                        </button>
                                        <button
                                            type="button"
                                            className="utilities-link utilities-repomaint-edit"
                                            data-testid={`github-repo-maint-edit-${rule.id}`}
                                            onClick={() => openEditor(rule)}
                                        >
                                            {text(lang, 'Edit', '编辑', '編輯')}
                                        </button>
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </section>
            {overlayOpen ? placeMaintDialogs(<>
            {editorOpen ? (
            <div className="utilities-repomaint-overlay utilities-repomaint-overlay--editor" data-testid="github-repo-maint-editor-dialog" data-maint-dialog="true" role="dialog" aria-modal="true" aria-labelledby="github-repo-maint-editor-title">
            <form className="utilities-repomaint-dialog utilities-repomaint-dialog--editor" data-testid="github-repo-maint-editor" onSubmit={(event) => { event.preventDefault(); void save(); }}>
                <div className="utilities-repomaint-dialog__head">
                    <span className="utilities-repomaint-dialog__icon utilities-repomaint-dialog__icon--primary" aria-hidden="true"><IconGitBranch /></span>
                    <h3 className="utilities-repomaint-dialog__title" id="github-repo-maint-editor-title">{draft.id ? text(lang, 'Edit rule', '编辑规则', '編輯規則') : text(lang, 'New rule', '新建规则', '新建規則')}</h3>
                    {draft.owner && draft.repo ? (
                        <span className="utilities-repomaint-editing-chip">{draft.owner}/{draft.repo}</span>
                    ) : null}
                </div>
                <div className="utilities-repomaint-editor-scroll">
                <div className="utilities-deploy-card__grid">
                    <label className="utilities-deploy-field utilities-deploy-field--full">
                        <span>{text(lang, 'Repository URL', '仓库地址', '倉庫網址')}</span>
                        <input
                            className="utilities-input utilities-repomaint-mono-input"
                            data-testid="github-repo-maint-repo-url"
                            placeholder="https://github.com/owner/repo"
                            value={draft.repo_url || ''}
                            autoFocus={!identityLocked}
                            readOnly={identityLocked}
                            onChange={(event) => setDraft({ ...draft, repo_url: event.target.value })}
                        />
                    </label>
                    <label className="utilities-deploy-field utilities-deploy-field--full">
                        <span>{text(lang, 'Local checkout', '本地目录', '本機目錄')}</span>
                        <span className="utilities-repomaint-dirrow">
                            <input
                                className="utilities-input utilities-repomaint-mono-input"
                                data-testid="github-repo-maint-ghost-dir"
                                placeholder={text(lang, 'Folder that mirrors the repository', '与仓库保持同步的本地目录', '與倉庫保持同步的本機目錄')}
                                value={draft.ghost_dir || ''}
                                readOnly={identityLocked}
                                onChange={(event) => setDraft({ ...draft, ghost_dir: event.target.value })}
                            />
                            <button type="button" className="utilities-btn utilities-repomaint-dirrow__pick" data-testid="github-repo-maint-pick-dir" disabled={identityLocked} onClick={() => { void pickDir(); }}>
                                {text(lang, 'Choose folder', '选择目录', '選擇目錄')}
                            </button>
                        </span>
                    </label>
                </div>
                {identityLocked ? (
                    <p className="utilities-hint" data-testid="github-repo-maint-identity-lock">{text(lang, 'Turn the master switch off before changing the repository or directory', '请先关闭总开关，再修改仓库或目录', '請先關閉總開關，再修改倉庫或目錄')}</p>
                ) : null}
                <p className="utilities-deploy-note">{text(lang, 'Each enabled event runs its command inside the local checkout when GitHub reports it.', 'GitHub 报告事件时，开启的事件会在本地目录里执行对应命令。', 'GitHub 報告事件時，開啟的事件會在本機目錄裡執行對應命令。')}</p>
                <div className="utilities-repomaint-events">
                    <p className="utilities-repomaint-group">
                        {text(lang, 'Issue events', '议题事件', '議題事件')}
                        <span className="utilities-repomaint-group__count">{armedIssueCount}/{ISSUE_EVENTS.length}</span>
                    </p>
                    {ISSUE_EVENTS.map(renderEvent)}
                    <p className="utilities-repomaint-group">
                        {text(lang, 'Code & check events', '代码与检查事件', '程式碼與檢查事件')}
                        <span className="utilities-repomaint-group__count">{armedCodeCount}/{CODE_EVENTS.length}</span>
                    </p>
                    {CODE_EVENTS.map(renderEvent)}
                </div>
                </div>
                <div className="utilities-repomaint-dialog__actions utilities-repomaint-editor-actions">
                    <span className="utilities-repomaint-editor-status">
                        <span className={`utilities-repomaint-armed-note${draftArmedCount > 0 ? '' : ' utilities-repomaint-armed-note--empty'}`}>
                            {draftArmedCount > 0
                                ? text(lang, `${draftArmedCount} events will run`, `将执行 ${draftArmedCount} 类事件`, `將執行 ${draftArmedCount} 類事件`)
                                : text(lang, 'No events enabled yet', '尚未启用任何事件', '尚未啟用任何事件')}
                        </span>
                        {error ? (
                            <span className="utilities-repomaint-save-error" role="alert">{error}</span>
                        ) : null}
                    </span>
                    <button type="button" className="utilities-btn" data-testid="github-repo-maint-editor-cancel" disabled={busy} onClick={closeEditor}>
                        {text(lang, 'Cancel', '取消', '取消')}
                    </button>
                    <button type="submit" className="utilities-btn utilities-btn--primary utilities-deploy-card__submit" data-testid="github-repo-maint-save" disabled={busy} aria-busy={busy || undefined}>
                        {text(lang, 'Save rule', '保存规则', '儲存規則')}
                    </button>
                </div>
            </form>
            </div>
            ) : null}
            {gate?.step === 'public' ? (
                <div className="utilities-repomaint-overlay" data-testid="github-repo-maint-public-dialog" data-maint-dialog="true" role="dialog" aria-modal="true" aria-labelledby="github-repo-maint-public-title">
                    <div className="utilities-repomaint-dialog">
                        <div className="utilities-repomaint-dialog__head">
                            <span className="utilities-repomaint-dialog__icon utilities-repomaint-dialog__icon--warn" aria-hidden="true"><IconWarn /></span>
                            <h3 className="utilities-repomaint-dialog__title" id="github-repo-maint-public-title">{text(lang, 'Enable on a public repository?', '要在公开仓库上启用吗？', '要在公開倉庫上啟用嗎？')}</h3>
                        </div>
                        <p className="utilities-repomaint-dialog__text">{text(lang, 'This is a public repository. Anyone can open an issue or comment, and that text starts a coding task.', '这是公开仓库。任何人都能开议题或写评论，那些文字会自动开工。', '這是公開倉庫。任何人都能開議題或寫評論，那些文字會自動開工。')}</p>
                        <div className="utilities-repomaint-dialog__actions">
                            <button type="button" className="utilities-btn" data-testid="github-repo-maint-public-cancel" disabled={busy} onClick={() => setGate(null)}>{text(lang, 'Cancel', '取消', '取消')}</button>
                            <button type="button" className="utilities-btn utilities-btn--primary" data-testid="github-repo-maint-public-confirm" disabled={busy} aria-busy={busy || undefined} autoFocus onClick={() => {
                                if (!gate.rule.hook_ready) setGate({ rule: gate.rule, step: 'choice' });
                                else void commitEnable(gate.rule, '');
                            }}>{text(lang, 'Continue', '继续', '繼續')}</button>
                        </div>
                    </div>
                </div>
            ) : null}
            {gate?.step === 'choice' ? (
                <div className="utilities-repomaint-overlay" data-testid="github-repo-maint-choice-dialog" data-maint-dialog="true" role="dialog" aria-modal="true" aria-labelledby="github-repo-maint-choice-title">
                    <div className="utilities-repomaint-dialog">
                        <div className="utilities-repomaint-dialog__head">
                            <span className="utilities-repomaint-dialog__icon utilities-repomaint-dialog__icon--primary" aria-hidden="true"><IconGitBranch /></span>
                            <h3 className="utilities-repomaint-dialog__title" id="github-repo-maint-choice-title">{text(lang, 'What should the first enable handle?', '第一次启用要处理哪些事件？', '第一次啟用要處理哪些事件？')}</h3>
                        </div>
                        <div className="utilities-repomaint-options">
                            <button type="button" className="utilities-repomaint-option" data-testid="github-repo-maint-choice-new" disabled={busy} autoFocus onClick={() => { void commitEnable(gate.rule, 'new_only'); }}>
                                <span className="utilities-repomaint-option__title">{text(lang, 'Only new events', '只处理之后的新事件', '只處理之後的新事件')}</span>
                                <span className="utilities-repomaint-option__desc">{text(lang, 'Skip what is already open and react from now on.', '跳过已存在的内容，从此刻开始响应。', '跳過已存在的內容，從此刻開始回應。')}</span>
                            </button>
                            <button type="button" className="utilities-repomaint-option" data-testid="github-repo-maint-choice-backfill" disabled={busy} onClick={() => { void commitEnable(gate.rule, 'backfill'); }}>
                                <span className="utilities-repomaint-option__title">{text(lang, 'Replay open items', '回放未处理的', '回放未處理的')}</span>
                                <span className="utilities-repomaint-option__desc">{text(lang, 'Also run commands on items that are still open.', '同时处理仍未关闭的议题和评论。', '同時處理仍未關閉的議題和評論。')}</span>
                            </button>
                        </div>
                        <div className="utilities-repomaint-dialog__actions">
                            <button type="button" className="utilities-btn" disabled={busy} onClick={() => setGate(null)}>{text(lang, 'Cancel', '取消', '取消')}</button>
                        </div>
                    </div>
                </div>
            ) : null}
            {deleteTarget ? (
                <div className="utilities-repomaint-overlay" data-testid="github-repo-maint-delete-dialog" data-maint-dialog="true" role="dialog" aria-modal="true" aria-labelledby="github-repo-maint-delete-title">
                    <div className="utilities-repomaint-dialog">
                        <div className="utilities-repomaint-dialog__head">
                            <span className="utilities-repomaint-dialog__icon utilities-repomaint-dialog__icon--warn" aria-hidden="true"><IconWarn /></span>
                            <h3 className="utilities-repomaint-dialog__title" id="github-repo-maint-delete-title">{text(lang, 'Delete this maintenance rule?', '删除这条维护规则？', '刪除這條維護規則？')}</h3>
                        </div>
                        <p className="utilities-repomaint-dialog__text">
                            {text(
                                lang,
                                `This removes the Hub rule and its GitHub webhook for ${deleteTarget.owner && deleteTarget.repo ? `${deleteTarget.owner}/${deleteTarget.repo}` : deleteTarget.repo_url || deleteTarget.id}. The local checkout and the repository itself are not touched.`,
                                `会同时删除 ${deleteTarget.owner && deleteTarget.repo ? `${deleteTarget.owner}/${deleteTarget.repo}` : deleteTarget.repo_url || deleteTarget.id} 的 Hub 规则和 GitHub webhook。本地目录与仓库本身不受影响。`,
                                `會同時刪除 ${deleteTarget.owner && deleteTarget.repo ? `${deleteTarget.owner}/${deleteTarget.repo}` : deleteTarget.repo_url || deleteTarget.id} 的 Hub 規則和 GitHub webhook。本機目錄與倉庫本身不受影響。`,
                            )}
                        </p>
                        <div className="utilities-repomaint-dialog__actions">
                            <button type="button" className="utilities-btn" data-testid="github-repo-maint-delete-cancel" disabled={busy} autoFocus onClick={() => setDeleteTarget(null)}>{text(lang, 'Cancel', '取消', '取消')}</button>
                            <button type="button" className="utilities-btn utilities-btn--danger" data-testid="github-repo-maint-delete-confirm" disabled={busy} aria-busy={busy || undefined} onClick={() => { void confirmDelete(); }}>{text(lang, 'Delete rule', '删除规则', '刪除規則')}</button>
                        </div>
                    </div>
                </div>
            ) : null}
            </>) : null}
        </div>
    );
}
