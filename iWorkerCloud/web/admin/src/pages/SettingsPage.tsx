import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { changePassword } from '../api/auth';

export function SettingsPage() {
  const { t } = useTranslation();
  const [username, setUsername] = useState('admin');
  const [oldPwd, setOldPwd] = useState('');
  const [newPwd, setNewPwd] = useState('');
  const [msg, setMsg] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await changePassword(username, oldPwd, newPwd);
      setMsg(t('common.success'));
      setOldPwd(''); setNewPwd('');
    } catch (err: any) { setMsg(err.message); }
  };

  return (
    <div>
      <div className="stage-card" style={{ maxWidth: 480 }}>
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
          <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} /></div>
          <div><label>{t('login.password')} (old)</label><input type="password" value={oldPwd} onChange={e => setOldPwd(e.target.value)} /></div>
          <div><label>{t('login.password')} (new)</label><input type="password" value={newPwd} onChange={e => setNewPwd(e.target.value)} /></div>
          {msg && <p style={{ fontSize: 13, margin: 0 }}>{msg}</p>}
          <button type="submit" className="btn-primary">{t('common.save')}</button>
        </form>
      </div>
    </div>
  );
}
