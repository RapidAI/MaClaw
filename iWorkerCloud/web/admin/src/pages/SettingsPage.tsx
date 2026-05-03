import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { changePassword } from '../api/auth';

export function SettingsPage() {
  const { t } = useTranslation();
  const [username, setUsername] = useState('admin');
  const [oldPwd, setOldPwd] = useState('');
  const [newPwd, setNewPwd] = useState('');
  const [msg, setMsg] = useState('');
  const [noticeTone, setNoticeTone] = useState<'ok' | 'danger' | 'info'>('info');
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      await changePassword(username.trim(), oldPwd, newPwd);
      setMsg(t('settings.passwordSaved'));
      setNoticeTone('ok');
      setOldPwd('');
      setNewPwd('');
    } catch (err: any) {
      setMsg(err.message);
      setNoticeTone('danger');
    } finally {
      setSaving(false);
    }
  };

  const canSubmit = username.trim() && oldPwd && newPwd && !saving;

  return (
    <div className="cloud-page-stack settings-page-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">{t('settings.eyebrow')}</div>
          <h3>{t('nav.settings')}</h3>
          <p>{t('settings.desc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('settings.boundaryTitle')}</strong>
          <span>{t('settings.boundaryNote')}</span>
        </div>
      </section>

      <div className="settings-grid">
        <div className="stage-card settings-form-card">
          <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
            <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} /></div>
            <div><label>{t('settings.oldPassword')}</label><input type="password" value={oldPwd} onChange={e => setOldPwd(e.target.value)} /></div>
            <div><label>{t('settings.newPassword')}</label><input type="password" value={newPwd} onChange={e => setNewPwd(e.target.value)} /></div>
            {msg && <div className={`notice ${noticeTone}`} style={{ margin: 0 }}>{msg}</div>}
            <button type="submit" className="btn-primary" disabled={!canSubmit}>{saving ? t('common.loading') : t('common.save')}</button>
          </form>
        </div>
        <section className="cloud-pillar-card card">
          <div className="mini">{t('settings.policyEyebrow')}</div>
          <h3>{t('settings.adminPolicyTitle')}</h3>
          <p>{t('settings.adminPolicyDesc')}</p>
          <div className="cloud-pill-list">
            <span>{t('settings.policyAuth')}</span>
            <span>{t('settings.policyNoBusiness')}</span>
            <span>{t('settings.policyAudit')}</span>
          </div>
        </section>
      </div>
    </div>
  );
}
