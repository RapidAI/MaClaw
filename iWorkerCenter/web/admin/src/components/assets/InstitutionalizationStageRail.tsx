import type { AssetNavigationTarget, CenterTab, OverviewNavigationTarget } from '../../types';

export type InstitutionalizationStageID = 'knowledge' | 'packages' | 'workflows';

export type InstitutionalizationStageCard = {
  id: InstitutionalizationStageID;
  eyebrow: string;
  title: string;
  tone: 'ok' | 'info' | 'warn';
  summary: string;
  detail: string;
  statLine: string[];
  actionLabel: string;
};

type InstitutionalizationStageRailProps = {
  activeStage: InstitutionalizationStageID;
  cards: InstitutionalizationStageCard[];
  navigationTarget?: AssetNavigationTarget;
  overviewTarget?: OverviewNavigationTarget | null;
  onNavigateToOverview?: (target?: OverviewNavigationTarget | null) => void;
  onNavigateToTab: (tab: CenterTab, target?: AssetNavigationTarget) => void;
};

const stageOrder: InstitutionalizationStageID[] = ['knowledge', 'packages', 'workflows'];

const stageRelationLabel = (activeStage: InstitutionalizationStageID, cardID: InstitutionalizationStageID) => {
  const activeIndex = stageOrder.indexOf(activeStage);
  const cardIndex = stageOrder.indexOf(cardID);
  if (activeIndex === cardIndex) {
    return 'Current';
  }
  return cardIndex < activeIndex ? 'Prior' : 'Next';
};

export function InstitutionalizationStageRail({
  activeStage,
  cards,
  navigationTarget,
  overviewTarget,
  onNavigateToOverview,
  onNavigateToTab,
}: InstitutionalizationStageRailProps) {
  return (
    <div className="asset-cockpit-grid">
      {cards.map((card) => {
        const isActive = card.id === activeStage;
        return (
          <section key={card.id} className={`card section-card asset-cockpit-card ${isActive ? 'asset-cockpit-card-active' : ''}`}>
            <div className="asset-cockpit-head">
              <div>
                <div className="mini light">{card.eyebrow}</div>
                <h3>{card.title}</h3>
              </div>
              <span className={`badge ${card.tone}`}>{stageRelationLabel(activeStage, card.id)}</span>
            </div>
            <strong>{card.summary}</strong>
            <p>{card.detail}</p>
            <div className="asset-cockpit-stats">
              {card.statLine.map((item) => <span key={item}>{item}</span>)}
            </div>
            <div className="executive-action-row">
              {isActive ? (
                <button type="button" className="executive-link-button" onClick={() => onNavigateToOverview?.(overviewTarget)}>
                  Return to overview
                </button>
              ) : (
                <button type="button" className="executive-assign-button" onClick={() => onNavigateToTab(card.id, navigationTarget)}>
                  {card.actionLabel}
                </button>
              )}
            </div>
          </section>
        );
      })}
    </div>
  );
}
