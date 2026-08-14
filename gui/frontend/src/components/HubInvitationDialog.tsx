import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { QRCodeSVG } from 'qrcode.react';
import { GetHubUserInvitationsPage, RotateHubUserInvitation } from '../../wailsjs/go/main/App';
import { usePortalThemeAttributes } from '../hooks/usePortalThemeAttributes';
import { useSafeBackdropDismiss } from '../hooks/useSafeBackdropDismiss';

type Invitee = { user_id?: string; contact?: string; registered_at?: string; status?: string };
type InvitationData = {
  enabled?: boolean; invite_url?: string; inviter_credits?: number; invitee_credits?: number;
  duration_days?: number; invitees?: Invitee[]; total?: number; page?: number; error?: string;
};

export function HubInvitationDialog({ open, onClose, lang }: { open: boolean; onClose: () => void; lang: string }) {
  const isSimplifiedChinese = lang === 'zh-Hans';
  const isTraditionalChinese = lang === 'zh-Hant';
  const t = (en: string, simplified: string, traditional = simplified) => isSimplifiedChinese ? simplified : isTraditionalChinese ? traditional : en;
  const dialogRef = useRef<HTMLElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const requestSerialRef = useRef(0);
  const dialogSessionRef = useRef(0);
  const onCloseRef = useRef(onClose);
  const [data, setData] = useState<InvitationData | null>(null);
  const [initialLoading, setInitialLoading] = useState(false);
  const [pageLoading, setPageLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [failedPage, setFailedPage] = useState<number | null>(null);
  const [copied, setCopied] = useState(false);
  const [rotating, setRotating] = useState(false);
  const [rotateError, setRotateError] = useState<string | null>(null);
  const [copyUnavailable, setCopyUnavailable] = useState(false);
  const portalThemeAttributes = usePortalThemeAttributes(open);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  const load = async (page: number, initial = false) => {
    const requestedPage = Math.max(1, Math.trunc(Number(page) || 1));
    const requestSerial = ++requestSerialRef.current;
    if (initial) setInitialLoading(true);
    else setPageLoading(true);
    setLoadError(null);
    setFailedPage(null);
    try {
      const next = await GetHubUserInvitationsPage(requestedPage) as InvitationData;
      if (requestSerial !== requestSerialRef.current) return;
      if (next.error) {
        if (initial) setData(null);
        setLoadError(next.error);
        setFailedPage(requestedPage);
        return;
      }
      setData(next);
    } catch (error) {
      if (requestSerial === requestSerialRef.current) {
        if (initial) setData(null);
        setLoadError(String(error));
        setFailedPage(requestedPage);
      }
    } finally {
      if (requestSerial === requestSerialRef.current) {
        if (initial) setInitialLoading(false);
        else setPageLoading(false);
      }
    }
  };

  const close = () => {
    requestSerialRef.current += 1;
    onCloseRef.current();
  };
  const { backdropProps, dialogProps } = useSafeBackdropDismiss<HTMLElement>(close);

  useEffect(() => {
    if (!open) return;
    const dialogSession = ++dialogSessionRef.current;
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setData(null);
    setLoadError(null);
    setFailedPage(null);
    setCopied(false);
    setRotating(false);
    setRotateError(null);
    setCopyUnavailable(false);
    void load(1, true);
    const timer = window.setTimeout(() => closeButtonRef.current?.focus(), 0);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        onCloseRef.current();
        return;
      }
      if (event.key !== 'Tab' || !dialogRef.current) return;
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      )).filter((element) => element.tabIndex >= 0 && !element.hasAttribute('hidden'));
      if (focusable.length === 0) {
        event.preventDefault();
        dialogRef.current.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('keydown', onKeyDown);
      requestSerialRef.current += 1;
      if (dialogSessionRef.current === dialogSession) dialogSessionRef.current += 1;
      if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
      restoreFocusRef.current = null;
    };
  }, [open]);

  if (!open) return null;

  const copy = async () => {
    if (!data?.invite_url) return;
    const dialogSession = dialogSessionRef.current;
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(data.invite_url);
      if (dialogSession !== dialogSessionRef.current) return;
      setCopied(true);
      setCopyUnavailable(false);
    } catch {
      if (dialogSession !== dialogSessionRef.current) return;
      // The link remains visible and selectable when the desktop clipboard is unavailable.
      setCopyUnavailable(true);
    }
  };

  const rotate = async () => {
    if (rotating || !window.confirm(t('Replace this link? The old invitation link will stop working.', '替换邀请链接？旧链接将立即失效。'))) return;
    const dialogSession = dialogSessionRef.current;
    setRotating(true);
    setRotateError(null);
    try {
      const next = await RotateHubUserInvitation() as InvitationData;
      if (dialogSession !== dialogSessionRef.current) return;
      if (next.enabled && next.invite_url) {
        setData((current) => ({ ...(current || {}), ...next }));
        setCopied(false);
        setCopyUnavailable(false);
      } else {
        if (next.enabled === false) setData(null);
        setRotateError(next.error || 'Unable to rotate invitation link');
      }
    } catch (error) {
      if (dialogSession !== dialogSessionRef.current) return;
      setRotateError(String(error));
    } finally {
      if (dialogSession === dialogSessionRef.current) setRotating(false);
    }
  };

  const page = Math.max(1, data?.page || 1);
  const total = Math.max(0, data?.total || 0);
  const totalPages = Math.max(1, Math.ceil(total / 20));

  const dialog = (
    <div
      className="hub-invitation-dialog__backdrop"
      role="presentation"
      {...portalThemeAttributes}
      {...backdropProps}
    >
      <section ref={dialogRef} className="hub-invitation-dialog" role="dialog" aria-modal="true" aria-labelledby="hub-invitation-dialog-title" tabIndex={-1} {...dialogProps}>
        <header className="hub-invitation-dialog__header">
          <div>
            <h2 id="hub-invitation-dialog-title">{t('Invite friends', '邀请好友')}</h2>
            <p>
              {initialLoading
                ? t('Fetching your invitation information…', '正在获取你的邀请信息…')
                : data?.enabled
                ? t(`A successful registration earns you ${data.inviter_credits || 0} Credits and your friend ${data.invitee_credits || 0} Credits.`, `好友成功注册后，你获得 ${data.inviter_credits || 0} Credits，好友获得 ${data.invitee_credits || 0} Credits。`)
        : t('Invitations are unavailable.', '邀请功能暂不可用。', '邀請功能暫不可用。')}
            </p>
          </div>
          <button ref={closeButtonRef} className="hub-invitation-dialog__close" type="button" onClick={close} aria-label={t('Close', '关闭')}>×</button>
        </header>

        {initialLoading && <div className="hub-invitation-dialog__loading" role="status">{t('Loading invitation information…', '正在加载邀请信息…')}</div>}
        {!initialLoading && loadError && <div className="hub-invitation-dialog__error" role="alert">
          <span>{t('Unable to load your invitation information. Please try again.', '无法加载邀请信息，请稍后重试。')}</span>
          <button className="hub-invitation-dialog__button" type="button" onClick={() => void load(failedPage || (data ? page : 1), !data)}>{t('Try again', '重试')}</button>
        </div>}

        {!initialLoading && data?.enabled && data.invite_url && <>
          <div className="hub-invitation-dialog__link-section">
            <div className="hub-invitation-dialog__link-copy">
              <label htmlFor="hub-invitation-link">{t('Your invitation link', '你的邀请链接')}</label>
              <div className="hub-invitation-dialog__link-actions">
                <input id="hub-invitation-link" readOnly value={data.invite_url} aria-label={t('Your invitation link', '你的邀请链接')} />
                <button className="hub-invitation-dialog__button hub-invitation-dialog__button--primary" type="button" onClick={() => void copy()}>{copied ? t('Copied', '已复制') : t('Copy', '复制')}</button>
                <button className="hub-invitation-dialog__button" type="button" onClick={() => void rotate()} disabled={rotating}>{rotating ? t('Rotating…', '正在替换…') : t('Rotate', '替换')}</button>
              </div>
              {rotateError && <p className="hub-invitation-dialog__rotate-error" role="alert">{t('Could not replace the invitation link. Your current link is still active.', '无法替换邀请链接，当前链接仍然有效。')}</p>}
              {copyUnavailable && <p className="hub-invitation-dialog__copy-hint" role="status">{t('Copy is unavailable here. Select the link and copy it manually.', '当前无法自动复制，请选中链接后手动复制。')}</p>}
              <p>{t(`Each reward expires ${data.duration_days || 30} days after the invited user registers.`, `每笔奖励从受邀用户注册成功起 ${data.duration_days || 30} 天内有效。`)}</p>
            </div>
            <div className="hub-invitation-dialog__qr" aria-label={t('Invitation link QR code', '邀请链接二维码')}>
              <QRCodeSVG value={data.invite_url} size={128} level="M" bgColor="#fff" fgColor="#101318" />
            </div>
          </div>

          <div className="hub-invitation-dialog__invitees">
            <div className="hub-invitation-dialog__invitees-heading">
              <h3>{t('Users you invited', '我邀请的用户')}</h3>
              <span>{t(`${total} total`, `共 ${total} 人`)}</span>
            </div>
            {(data.invitees || []).length === 0 ? (
              <p className="hub-invitation-dialog__empty">{t('Share your link to invite your first friend.', '分享邀请链接，邀请第一位好友。')}</p>
            ) : (
              <div className="hub-invitation-dialog__invitee-grid">
                {(data.invitees || []).map((invitee, index) => (
                  <article key={invitee.user_id || index} className="hub-invitation-dialog__invitee">
                    <strong title={invitee.contact || invitee.user_id}>{invitee.contact || invitee.user_id}</strong>
                    <span>{t('Registered: ', '受邀注册：')}{invitee.registered_at ? new Date(invitee.registered_at).toLocaleString() : '-'}</span>
                  </article>
                ))}
              </div>
            )}
            {totalPages > 1 && <nav className="hub-invitation-dialog__pagination" aria-label={t('Invited users pages', '受邀用户分页')} aria-busy={pageLoading}>
              <button className="hub-invitation-dialog__button" type="button" disabled={page <= 1 || pageLoading} onClick={() => void load(page - 1)}>{t('Previous', '上一页')}</button>
              <span>{t(`Page ${page} / ${totalPages}`, `第 ${page} / ${totalPages} 页`)}</span>
              <button className="hub-invitation-dialog__button" type="button" disabled={page >= totalPages || pageLoading} onClick={() => void load(page + 1)}>{t('Next', '下一页')}</button>
              {pageLoading && <span className="hub-invitation-dialog__pagination-loading" role="status">{t('Loading…', '正在加载…')}</span>}
            </nav>}
          </div>
        </>}
      </section>
    </div>
  );

  // This component is triggered from the sidebar, beneath the DPI scaling
  // layer. A fixed overlay inside that transformed ancestor is positioned
  // against the scaled layer instead of the window, leaving parts of the
  // background uncovered. Render beside that layer so the backdrop always
  // owns the whole application viewport.
  if (typeof document === 'undefined') return dialog;
  const overlayHost = document.querySelector<HTMLElement>('.app-viewport') || document.body;
  return createPortal(dialog, overlayHost);
}
