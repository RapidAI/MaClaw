import { useEffect, useRef, useState } from 'react';
import { DesktopBotAccess, GetHubUserInvitationStatus } from '../../../wailsjs/go/main/App';
import { beginBotAccessRead, isCurrentBotAccessRead, subscribeBotAccess } from '../bots/botOpenGate';

const HUB_INVITATION_STATUS_REFRESH_INTERVAL_MS = 30_000;

export function useSidebarHubAccess(opts: {
    activated: boolean;
    navTab: string;
    switchTool: (tool: string) => void;
    remoteHubUrl?: string;
    remoteViewerToken?: string;
    remoteTenantId?: string;
}) {
    const [invitationEnabled, setInvitationEnabled] = useState(false);
    const [invitationDialogOpen, setInvitationDialogOpen] = useState(false);
    const [botAllowed, setBotAllowed] = useState(false);
    const invitationRequestSeqRef = useRef(0);
    const botAccessRequestSeqRef = useRef(0);
    const botAccessGrantedRef = useRef(false);
    const botAccessFailuresRef = useRef(0);
    const navTabRef = useRef(opts.navTab);
    const switchToolRef = useRef(opts.switchTool);
    navTabRef.current = opts.navTab;
    switchToolRef.current = opts.switchTool;

    useEffect(() => subscribeBotAccess(enabled => {
        botAccessGrantedRef.current = enabled;
        botAccessFailuresRef.current = 0;
        setBotAllowed(enabled);
    }), []);

    // Hub can change invitation and Bot grants while MaClaw is open. One
    // foreground refresh covers both, and a hidden window does not poll.
    useEffect(() => {
        if (!opts.activated) {
            invitationRequestSeqRef.current += 1;
            botAccessRequestSeqRef.current += 1;
            botAccessGrantedRef.current = false;
            botAccessFailuresRef.current = 0;
            setInvitationEnabled(false);
            setInvitationDialogOpen(false);
            setBotAllowed(false);
            if (navTabRef.current === 'bots') switchToolRef.current('ai');
            return;
        }
        let cancelled = false;
        const refreshHubAccess = () => {
            if (document.visibilityState === 'hidden') return;
            const invitationSeq = ++invitationRequestSeqRef.current;
            GetHubUserInvitationStatus().then((result: { enabled?: boolean; error?: string } | null) => {
                if (cancelled || invitationSeq !== invitationRequestSeqRef.current) return;
                const enabled = !!result?.enabled && !result?.error;
                setInvitationEnabled(enabled);
                if (!enabled) setInvitationDialogOpen(false);
            }).catch(() => {
                if (cancelled || invitationSeq !== invitationRequestSeqRef.current) return;
                setInvitationEnabled(false);
                setInvitationDialogOpen(false);
            });
            const botSeq = ++botAccessRequestSeqRef.current;
            const accessRead = beginBotAccessRead();
            DesktopBotAccess().then((result: { enabled?: boolean; message?: string } | null) => {
                if (cancelled || botSeq !== botAccessRequestSeqRef.current || !isCurrentBotAccessRead(accessRead)) return;
                const enabled = !!result?.enabled;
                botAccessFailuresRef.current = 0;
                botAccessGrantedRef.current = enabled;
                setBotAllowed(enabled);
                if (!enabled && navTabRef.current === 'bots') switchToolRef.current('ai');
            }).catch(() => {
                if (cancelled || botSeq !== botAccessRequestSeqRef.current || !isCurrentBotAccessRead(accessRead)) return;
                botAccessFailuresRef.current += 1;
                if (botAccessGrantedRef.current && botAccessFailuresRef.current < 2) return;
                botAccessGrantedRef.current = false;
                setBotAllowed(false);
                if (navTabRef.current === 'bots') switchToolRef.current('ai');
            });
        };
        const onVisibilityChange = () => {
            if (document.visibilityState === 'visible') refreshHubAccess();
        };
        refreshHubAccess();
        const interval = window.setInterval(refreshHubAccess, HUB_INVITATION_STATUS_REFRESH_INTERVAL_MS);
        document.addEventListener('visibilitychange', onVisibilityChange);
        return () => {
            cancelled = true;
            window.clearInterval(interval);
            document.removeEventListener('visibilitychange', onVisibilityChange);
        };
    }, [opts.activated, opts.remoteHubUrl, opts.remoteViewerToken, opts.remoteTenantId]);

    // The click path re-verifies access in App.switchTool (fresh DesktopBotAccess
    // read + denial toast), so the rail only forwards the navigation.
    const openBots = () => {
        opts.switchTool('bots');
    };

    // The rail hides the Bot entry until Hub grants access. botAllowed starts
    // false, so the entry is absent during the first access read instead of
    // flashing and disappearing when the denial lands.
    return { invitationEnabled, invitationDialogOpen, setInvitationDialogOpen, openBots, botAllowed };
}
