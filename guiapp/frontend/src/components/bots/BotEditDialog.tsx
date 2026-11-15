import { useEffect, useId, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { BotAvatar } from './BotAvatar';
import { BOT_COLORS, BOT_SHAPES, type BotAppearance, type BotShape } from './botAppearance';

export type BotEditDialogLabels = {
    title: string;
    name: string;
    description: string;
    save: string;
    cancel: string;
    close: string;
    color?: string;
    shape?: string;
    /** Shown when another bot already uses the name. */
    nameTaken?: string;
};

/** One local server the new bot can run on. */
export type BotServerChoice = {
    id: string;
    label: string;
};

/** Create-only: whether this bot should use a computer, plus the quota line. */
export type BotComputerChoice = {
    label: string;
    quota: string;
    /** True when the grant is already used up, so a new bot cannot take a computer. */
    locked: boolean;
    /** Local servers with MaClawSrv. Empty hides the picker. */
    servers?: BotServerChoice[];
    /** Initially selected server. Empty is the cloud computer. */
    server?: string;
    cloudLabel?: string;
    localLabel?: string;
    serverLegend?: string;
    /** Shown when local is selected but that server is no longer listed. */
    serverMissing?: string;
};

function initialComputerChoice(computer?: BotComputerChoice): { place: 'cloud' | 'local'; server: string } {
    const servers = computer?.servers ?? [];
    const next = (computer?.server || '').trim();
    if (servers.some(item => item.id === next)) return { place: 'local', server: next };
    return { place: 'cloud', server: servers[0]?.id || '' };
}

type BotEditDialogProps = {
    initialTitle: string;
    initialDescription: string;
    labels: BotEditDialogLabels;
    /** Save failure shown inside the dialog; the dialog stays open for another try. */
    notice: string;
    saving: boolean;
    onClose: () => void;
    onSubmit: (title: string, description: string, look: BotAppearance, needsComputer?: boolean, serverID?: string) => void;
    initialAppearance?: BotAppearance;
    /** Returns true when the name is already used by another bot. */
    isNameTaken?: (title: string) => boolean;
    testId?: string;
    /** Present only while creating. Editing a bot does not change this here. */
    computer?: BotComputerChoice;
};

// Same caps as the rename inputs this dialog replaces.
const TITLE_MAX = 40;
const DESCRIPTION_MAX = 80;

/**
 * Modal editor for a bot's name and description. Rendered inside #App, so the
 * custom-dialog classes pick up the theme variables directly.
 */
export function BotEditDialog({ initialTitle, initialDescription, labels, notice, saving, onClose, onSubmit, initialAppearance, isNameTaken, testId, computer }: BotEditDialogProps) {
    const titleId = useId();
    const nameId = useId();
    const descriptionId = useId();
    const [title, setTitle] = useState(initialTitle);
    const [description, setDescription] = useState(initialDescription);
    const titleRef = useRef<HTMLInputElement | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const previousFocusRef = useRef<HTMLElement | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !saving });
    const [color, setColor] = useState(initialAppearance?.color || BOT_COLORS[0]);
    const [shape, setShape] = useState<BotShape>(initialAppearance?.shape || 'circle');
    const [needsComputer, setNeedsComputer] = useState(computer ? !computer.locked : true);
    const openingChoice = initialComputerChoice(computer);
    const [place, setPlace] = useState<'cloud' | 'local'>(openingChoice.place);
    const [server, setServer] = useState(openingChoice.server);
    const [choiceError, setChoiceError] = useState('');
    const choiceTouched = useRef(false);
    useEffect(() => {
        if (computer?.locked) setNeedsComputer(false);
    }, [computer?.locked]);
    useEffect(() => {
        if (choiceTouched.current) return;
        const servers = computer?.servers ?? [];
        const next = (computer?.server || '').trim();
        if (servers.some(item => item.id === next)) {
            setPlace('local');
            setServer(next);
            return;
        }
        setPlace('cloud');
        setServer(servers[0]?.id || '');
    }, [computer?.server, computer?.servers]);
    const taken = !!isNameTaken && title.trim() !== '' && isNameTaken(title.trim());
    const canSave = title.trim() !== '' && !taken && !saving;

    useEffect(() => {
        previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const focusTimer = window.setTimeout(() => {
            titleRef.current?.focus();
            titleRef.current?.select();
        }, 0);
        return () => {
            window.clearTimeout(focusTimer);
            const previousFocus = previousFocusRef.current;
            previousFocusRef.current = null;
            if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true });
        };
    }, []);

    // Keep keyboard focus inside the modal while it is open, as CustomDialog does.
    useEffect(() => {
        const handleFocusIn = (event: FocusEvent) => {
            if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
                titleRef.current?.focus();
            }
        };
        document.addEventListener('focusin', handleFocusIn, true);
        return () => document.removeEventListener('focusin', handleFocusIn, true);
    }, []);

    const submit = () => {
        if (!canSave) return;
        const servers = computer?.servers ?? [];
        if (needsComputer && servers.length > 0) {
            let picked = '';
            if (place === 'local') {
                picked = servers.find(item => item.id === server)?.id || '';
                if (!picked && servers.length === 1) picked = servers[0].id;
                if (!picked) {
                    setChoiceError(computer?.serverMissing || 'Choose a local computer');
                    return;
                }
            }
            setChoiceError('');
            onSubmit(title.trim(), description.trim(), { color, shape }, true, picked);
            return;
        }
        onSubmit(title.trim(), description.trim(), { color, shape }, computer ? needsComputer : undefined);
    };

    const onKeyDown = (event: ReactKeyboardEvent) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            if (!saving) onClose();
            return;
        }
        if (event.key === 'Enter') {
            // IME composition and a focused button own Enter.
            const native = event.nativeEvent as KeyboardEvent;
            if (native.isComposing || native.keyCode === 229) return;
            if (event.target instanceof HTMLButtonElement) return;
            event.preventDefault();
            event.stopPropagation();
            submit();
            return;
        }
        if (event.key === 'Tab') {
            const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
                'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [href]',
            );
            if (!focusable?.length) return;
            const items = Array.from(focusable);
            const currentIndex = items.indexOf(document.activeElement as HTMLElement);
            const nextIndex = event.shiftKey
                ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
            event.preventDefault();
            items[nextIndex].focus();
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-edit-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div
                className="modal-content custom-dialog bot-edit-dialog"
                ref={dialogRef}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-testid={testId || "bot-edit-dialog"}
                {...dialogProps}
            >
                <div className="modal-header custom-dialog__header">
                    <div className="custom-dialog__heading">
                        <h3 id={titleId} className="custom-dialog__title">{labels.title}</h3>
                    </div>
                    <button type="button" className="btn-close custom-dialog__close" aria-label={labels.close} disabled={saving} onClick={onClose}>
                        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
                            <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                        </svg>
                    </button>
                </div>
                <div className="modal-body custom-dialog__body">
                    <div className="bot-edit-dialog__field">
                        <label className="form-label bot-edit-dialog__label" htmlFor={nameId}>{labels.name}</label>
                        <input
                            id={nameId}
                            ref={titleRef}
                            className="form-input bot-edit-dialog__input"
                            type="text"
                            value={title}
                            maxLength={TITLE_MAX}
                            onChange={event => setTitle(event.target.value)}
                            autoComplete="off"
                            spellCheck={false}
                            disabled={saving}
                        />
                    </div>
                    <div className="bot-edit-dialog__field">
                        <label className="form-label bot-edit-dialog__label" htmlFor={descriptionId}>{labels.description}</label>
                        <input
                            id={descriptionId}
                            className="form-input bot-edit-dialog__input"
                            type="text"
                            value={description}
                            maxLength={DESCRIPTION_MAX}
                            onChange={event => setDescription(event.target.value)}
                            autoComplete="off"
                            spellCheck={false}
                            disabled={saving}
                        />
                    </div>
                    {taken ? <p className="bot-edit-dialog__notice" role="alert" data-testid="bot-edit-name-taken">{labels.nameTaken || 'Name already used'}</p> : null}
                    <div className="bot-edit-dialog__field bot-edit-dialog__look">
                        <BotAvatar color={color} shape={shape} className="bot-edit-dialog__preview" testId="bot-edit-preview" />
                    </div>
                    <div className="bot-edit-dialog__field" role="radiogroup" aria-label={labels.color || 'Color'}>
                        <span className="form-label bot-edit-dialog__label">{labels.color || 'Color'}</span>
                        <div className="bot-edit-dialog__swatches">
                            {BOT_COLORS.map(c => (
                                <button
                                    key={c}
                                    type="button"
                                    role="radio"
                                    aria-checked={c === color}
                                    aria-label={c}
                                    data-testid={'bot-edit-color-' + c.slice(1)}
                                    className={'bot-edit-dialog__swatch' + (c === color ? ' is-selected' : '')}
                                    style={{ background: c }}
                                    disabled={saving}
                                    onClick={() => setColor(c)}
                                />
                            ))}
                        </div>
                    </div>
                    <div className="bot-edit-dialog__field" role="radiogroup" aria-label={labels.shape || 'Shape'}>
                        <span className="form-label bot-edit-dialog__label">{labels.shape || 'Shape'}</span>
                        <div className="bot-edit-dialog__shapes">
                            {BOT_SHAPES.map(sh => (
                                <button
                                    key={sh}
                                    type="button"
                                    role="radio"
                                    aria-checked={sh === shape}
                                    aria-label={sh}
                                    data-testid={'bot-edit-shape-' + sh}
                                    className={'bot-edit-dialog__shape' + (sh === shape ? ' is-selected' : '')}
                                    disabled={saving}
                                    onClick={() => setShape(sh)}
                                >
                                    <BotAvatar color={color} shape={sh} />
                                </button>
                            ))}
                        </div>
                    </div>
                    {computer ? (
                        <div className="bot-edit-dialog__field">
                            <label className="bot-edit-dialog__computer">
                                <input
                                    type="checkbox"
                                    data-testid="bot-create-needs-computer"
                                    checked={needsComputer}
                                    disabled={saving || computer.locked}
                                    onChange={event => setNeedsComputer(event.target.checked)}
                                />
                                <span>{computer.label}</span>
                            </label>
                            {computer.quota ? <p className="bot-edit-dialog__quota" data-testid="bot-create-quota">{computer.quota}</p> : null}
                            {needsComputer && computer.servers && computer.servers.length > 0 ? (
                                <div className="bot-edit-dialog__servers" data-testid="bot-create-servers">
                                    <div role="radiogroup" aria-label={computer.serverLegend || 'Server'}>
                                        <span className="form-label bot-edit-dialog__label">{computer.serverLegend || 'Server'}</span>
                                        <label className="bot-edit-dialog__computer">
                                            <input
                                                type="radio"
                                                name="bot-create-place"
                                                data-testid="bot-create-place-cloud"
                                                checked={place === 'cloud'}
                                                disabled={saving}
                                                onChange={() => { choiceTouched.current = true; setChoiceError(''); setPlace('cloud'); }}
                                            />
                                            <span>{computer.cloudLabel || 'Cloud computer'}</span>
                                        </label>
                                        <label className="bot-edit-dialog__computer">
                                            <input
                                                type="radio"
                                                name="bot-create-place"
                                                data-testid="bot-create-place-local"
                                                checked={place === 'local'}
                                                disabled={saving}
                                                onChange={() => {
                                                    choiceTouched.current = true;
                                                    setChoiceError('');
                                                    setPlace('local');
                                                    if (!(computer.servers ?? []).some(item => item.id === server)) setServer(computer.servers?.[0]?.id || '');
                                                }}
                                            />
                                            <span>{computer.localLabel || 'Local computer'}</span>
                                            {computer.servers.length === 1 ? <span>{' · '}<span data-testid="bot-create-local-only">{computer.servers[0].label}</span></span> : null}
                                        </label>
                                    </div>
                                    {place === 'local' && computer.servers.length > 1 ? (
                                        <div className="bot-edit-dialog__servers" role="radiogroup" aria-label={computer.localLabel || 'Local computer'} data-testid="bot-create-local-servers">
                                            {computer.servers.map(item => (
                                                <label key={item.id} className="bot-edit-dialog__computer">
                                                    <input
                                                        type="radio"
                                                        name="bot-create-server"
                                                        data-testid={`bot-create-server-${item.id}`}
                                                        checked={server === item.id}
                                                        disabled={saving}
                                                        onChange={() => { choiceTouched.current = true; setChoiceError(''); setServer(item.id); }}
                                                    />
                                                    <span>{item.label}</span>
                                                </label>
                                            ))}
                                        </div>
                                    ) : null}
                                    {choiceError ? <p className="bot-edit-dialog__notice" role="alert" data-testid="bot-create-server-missing">{choiceError}</p> : null}
                                </div>
                            ) : null}
                        </div>
                    ) : null}
                    {notice ? <p className="bot-edit-dialog__notice" role="status">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={saving}>{labels.cancel}</button>
                    <button type="button" className="btn-primary" data-testid="bot-edit-save" onClick={submit} disabled={!canSave}>{labels.save}</button>
                </div>
            </div>
        </div>
    );
}
