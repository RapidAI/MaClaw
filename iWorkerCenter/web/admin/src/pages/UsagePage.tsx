import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { listDiWorkerUsage, type DiWorkerUsage } from '../api/compute';

const today = () => new Date().toISOString().slice(0, 10);
const monthAgo = () => { const d = new Date(); d.setMonth(d.getMonth() - 1); return d.toISOString().slice(0, 10); };

export function UsagePage() {
  const { t } = useTranslation();
  const [period, setPeriod] = useState('daily');
  const [dateStart, setDateStart] = useState(monthAgo());
  const [dateEnd, setDateEnd] = useState(today());
  const [rows, setRows] = useState<DiWorkerUsage[]>([]);
  const [loaded, setLoaded] = useState(false);

  const load = () => {
    listDiWorkerUsage(period, dateStart, dateEnd)
      .then(d => { setRows(d ?? []); setLoaded(true); })
      .catch(() => { setRows([]); setLoaded(true); });
  };

  const totalCost = rows.reduce((s, r) => s + r.total_cost, 0);
  const totalTokens = rows.reduce((s, r) => s + r.total_tokens, 0);

  return (
    <div className="center-page-stack">
      <SectionCard title={t('usage.title')} desc={t('subtitle.usage')}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end', marginBottom: 16, flexWrap: 'wrap' }}>
          <div><label>{t('usage.period')}</label>
            <select value={period} onChange={e => setPeriod(e.target.value)}>
              <option value="daily">{t('usage.daily')}</option>
              <option value="monthly">{t('usage.monthly')}</option>
            </select>
          </div>
          <div><label>{t('usage.startDate')}</label><input type="date" value={dateStart} onChange={e => setDateStart(e.target.value)} /></div>
          <div><label>{t('usage.endDate')}</label><input type="date" value={dateEnd} onChange={e => setDateEnd(e.target.value)} /></div>
          <button className="btn-primary" onClick={load}>{t('usage.query')}</button>
        </div>

        {!loaded ? (
          <div className="hint">{t('usage.selectDateRange')}</div>
        ) : rows.length === 0 ? (
          <div className="hint">{t('common.noData')}</div>
        ) : (
          <>
            <div className="card" style={{ padding: 12, marginBottom: 12, fontWeight: 600 }}>
              {t('usage.totalCost')}: ¥{totalCost.toFixed(4)}
              {' | '}{t('usage.totalTokens')}: {totalTokens.toLocaleString()}
              {' | '}{t('usage.totalRequests')}: {rows.reduce((s, r) => s + r.request_count, 0).toLocaleString()}
            </div>
            <table className="data-table" style={{ width: '100%' }}>
              <thead><tr>
                <th>{t('usage.diworkerName')}</th>
                <th>{t('usage.inputTokens')}</th><th>{t('usage.outputTokens')}</th><th>{t('usage.totalTokens')}</th>
                <th>{t('usage.inputCost')}</th><th>{t('usage.outputCost')}</th><th>{t('usage.totalCost')}</th>
                <th>{t('usage.requests')}</th>
              </tr></thead>
              <tbody>
                {rows.map((r, i) => (
                  <tr key={i}>
                    <td>{r.diworker_name || r.diworker_id}</td>
                    <td>{r.total_input_tokens.toLocaleString()}</td>
                    <td>{r.total_output_tokens.toLocaleString()}</td>
                    <td>{r.total_tokens.toLocaleString()}</td>
                    <td>¥{r.input_cost.toFixed(4)}</td>
                    <td>¥{r.output_cost.toFixed(4)}</td>
                    <td>¥{r.total_cost.toFixed(4)}</td>
                    <td>{r.request_count.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </SectionCard>
    </div>
  );
}
