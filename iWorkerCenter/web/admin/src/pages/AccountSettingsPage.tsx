import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { fetchProfile, updatePassword, updateProfile } from '../api/account';

export function AccountSettingsPage() {
  const { t } = useTranslation();
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [loading, setLoading] = useState(true);
  const [savingProfile, setSavingProfile] = useState(false);
  const [savingPassword, setSavingPassword] = useState(false);
  const [message, setMessage] = useState('');

  const load = () => {
    setLoading(true);
    setMessage('');
    fetchProfile()
      .then(profile => {
        setUsername(profile.username || '');
        setEmail(profile.email || '');
      })
      .catch(err => setMessage(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleSaveProfile = async () => {
    setSavingProfile(true);
    setMessage('');
    try {
      await updateProfile(email);
      setMessage(t('account.profileSaved'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingProfile(false);
    }
  };

  const handleSavePassword = async () => {
    setMessage('');
    if (newPassword !== confirmPassword) {
      setMessage(t('account.passwordMismatch'));
      return;
    }
    setSavingPassword(true);
    try {
      await updatePassword(oldPassword, newPassword);
      setOldPassword('');
      setNewPassword('');
      setConfirmPassword('');
      setMessage(t('account.passwordSaved'));
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingPassword(false);
    }
  };

  return (
    <div className="center-page-stack account-page">
      {message ? <div className="hint">{message}</div> : null}
      <div className="panel-grid account-grid">
        <SectionCard title={t('account.profileTitle')} desc={loading ? t('common.loading') : t('account.profileDesc')}>
          <div className="account-form">
            <label><span>{t('account.username')}</span><input value={username} disabled /></label>
            <label><span>{t('account.email')}</span><input value={email} onChange={event => setEmail(event.target.value)} placeholder="admin@example.com" /></label>
            <div className="account-actions">
              <button type="button" className="btn-primary" disabled={savingProfile} onClick={handleSaveProfile}>{savingProfile ? t('common.loading') : t('common.save')}</button>
              <button type="button" className="btn-ghost" onClick={load}>{t('common.refresh')}</button>
            </div>
          </div>
        </SectionCard>

        <SectionCard title={t('account.passwordTitle')} desc={t('account.passwordDesc')}>
          <div className="account-form">
            <label><span>{t('account.oldPassword')}</span><input type="password" value={oldPassword} onChange={event => setOldPassword(event.target.value)} /></label>
            <label><span>{t('account.newPassword')}</span><input type="password" value={newPassword} onChange={event => setNewPassword(event.target.value)} /></label>
            <label><span>{t('account.confirmPassword')}</span><input type="password" value={confirmPassword} onChange={event => setConfirmPassword(event.target.value)} /></label>
            <div className="account-actions">
              <button type="button" className="btn-primary" disabled={savingPassword || !oldPassword || !newPassword} onClick={handleSavePassword}>{savingPassword ? t('common.loading') : t('account.changePassword')}</button>
            </div>
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
