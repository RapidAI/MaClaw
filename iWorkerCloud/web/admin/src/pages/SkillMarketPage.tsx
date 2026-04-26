import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

type MarketItem = {
  title: string;
  stage: 'draft' | 'listed' | 'controlled';
  summary: string;
  detail: string;
  tags: string[];
};

export function SkillMarketPage() {
  const { t } = useTranslation();
  const items = useMemo<MarketItem[]>(() => [
    {
      title: t('skills.items.executive.title'),
      stage: 'controlled',
      summary: t('skills.items.executive.summary'),
      detail: t('skills.items.executive.detail'),
      tags: [t('skills.tagBoard'), t('skills.tagReport'), t('skills.tagDecision')],
    },
    {
      title: t('skills.items.recovery.title'),
      stage: 'listed',
      summary: t('skills.items.recovery.summary'),
      detail: t('skills.items.recovery.detail'),
      tags: [t('skills.tagWorkflow'), t('skills.tagMemory'), t('skills.tagRollout')],
    },
    {
      title: t('skills.items.compute.title'),
      stage: 'draft',
      summary: t('skills.items.compute.summary'),
      detail: t('skills.items.compute.detail'),
      tags: [t('skills.tagCompute'), t('skills.tagQuota'), t('skills.tagCost')],
    },
  ], [t]);

  return (
    <div className="cloud-overview-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">Skill Market</div>
          <h3>{t('skills.title')}</h3>
          <p>{t('skills.desc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('skills.positionTitle')}</strong>
          <span>{t('skills.positionDesc')}</span>
        </div>
      </section>

      <div className="cloud-market-grid">
        {items.map((item) => (
          <section key={item.title} className="cloud-pillar-card card">
            <div className="item-head">
              <div>
                <span className="mini">{t(`skills.stage.${item.stage}`)}</span>
                <h3>{item.title}</h3>
              </div>
              <span className={`badge ${item.stage === 'draft' ? 'warn' : item.stage === 'listed' ? 'info' : 'ok'}`}>
                {t(`skills.badge.${item.stage}`)}
              </span>
            </div>
            <strong>{item.summary}</strong>
            <p>{item.detail}</p>
            <div className="cloud-pill-list">
              {item.tags.map((tag) => <span key={tag}>{tag}</span>)}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
