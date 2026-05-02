import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { createBundle, listBundles, publishBundle, type ConfigBundle } from '../api/delivery';

const defaultPayload = `{
  "features": {
    "mcp_sync": true,
    "skill_delivery": true
  },
  "notes": "Configuration bundle managed by iWorkerCenter"
}`;

function formatDate(value?: string) {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function previewPayload(payload: string) {
  if (!payload) return '-';
  try {
    return JSON.stringify(JSON.parse(payload), null, 2);
  } catch {
    return payload;
  }
}

export function DeliveryPage() {
  const { t } = useTranslation();
  const [bundles, setBundles] = useState<ConfigBundle[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [contentType, setContentType] = useState('full');
  const [note, setNote] = useState('');
  const [payloadText, setPayloadText] = useState(defaultPayload);
  const [message, setMessage] = useState('');
  const [saving, setSaving] = useState(false);
  const [publishingId, setPublishingId] = useState('');

  const load = () => {
    setLoading(true);
    listBundles()
      .then(setBundles)
      .catch(err => setMessage(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async () => {
    setSaving(true);
    setMessage('');
    try {
      let payload: unknown;
      try {
        payload = JSON.parse(payloadText);
      } catch {
        setMessage(t('delivery.invalidJson'));
        return;
      }
      await createBundle({ content_type: contentType, payload, note });
      setShowForm(false);
      setNote('');
      setPayloadText(defaultPayload);
      setMessage(t('delivery.created'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const handlePublish = async (bundle: ConfigBundle) => {
    setPublishingId(bundle.id);
    setMessage('');
    try {
      await publishBundle(bundle.id);
      setMessage(t('delivery.published'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setPublishingId('');
    }
  };

  const latestPublished = bundles.find(bundle => bundle.status === 'published');

  return (
    <div className="center-page-stack delivery-page">
      {message ? <div className="hint">{message}</div> : null}
      <SectionCard title={t('delivery.title')} desc={loading ? t('common.loading') : t('delivery.desc')}>
        <div className="delivery-toolbar">
          <div className="cloud-pill-list">
            <span>{t('delivery.total')}: {bundles.length}</span>
            <span>{t('delivery.latestPublished')}: {latestPublished ? latestPublished.version : '-'}</span>
          </div>
          <div className="actions">
            <button className="btn-ghost" type="button" onClick={load}>{t('common.refresh')}</button>
            <button className="btn-primary" type="button" onClick={() => setShowForm(current => !current)}>{t('delivery.newBundle')}</button>
          </div>
        </div>

        {showForm ? <div className="delivery-editor card">
          <div className="delivery-editor-grid">
            <label><span>{t('delivery.contentType')}</span><select value={contentType} onChange={event => setContentType(event.target.value)}><option value="full">full</option><option value="incremental">incremental</option></select></label>
            <label><span>{t('delivery.note')}</span><input value={note} onChange={event => setNote(event.target.value)} /></label>
            <label className="field-span-2"><span>{t('delivery.payload')}</span><textarea value={payloadText} onChange={event => setPayloadText(event.target.value)} rows={10} /></label>
          </div>
          <div className="actions">
            <button className="btn-primary" disabled={saving} onClick={handleCreate}>{saving ? t('common.loading') : t('common.create')}</button>
            <button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button>
          </div>
        </div> : null}

        {bundles.length === 0 ? <div className="hint">{t('delivery.empty')}</div> : <div className="delivery-list">
          {bundles.map(bundle => (
            <div key={bundle.id} className="item-row delivery-card">
              <div className="item-head">
                <div><strong>v{bundle.version}</strong><p>{bundle.note || bundle.id}</p></div>
                <span className={bundle.status === 'published' ? 'badge ok' : 'badge info'}>{bundle.status}</span>
              </div>
              <div className="cloud-pill-list">
                <span>{bundle.content_type}</span>
                <span>{t('delivery.createdAt')}: {formatDate(bundle.created_at)}</span>
                <span>{t('delivery.publishedAt')}: {formatDate(bundle.published_at)}</span>
              </div>
              <details className="payload-preview"><summary>{t('delivery.viewPayload')}</summary><pre>{previewPayload(bundle.payload)}</pre></details>
              <div className="actions">
                {bundle.status !== 'published' ? <button className="btn-primary" disabled={publishingId === bundle.id} onClick={() => handlePublish(bundle)}>{publishingId === bundle.id ? t('common.loading') : t('delivery.publish')}</button> : null}
              </div>
            </div>
          ))}
        </div>}
      </SectionCard>
    </div>
  );
}
