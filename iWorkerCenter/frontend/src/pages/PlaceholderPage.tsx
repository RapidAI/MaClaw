import { EmptyState } from '../components/feedback/EmptyState';

type Props = {
  title: string;
  desc: string;
};

export function PlaceholderPage({ title, desc }: Props) {
  return (
    <div className="center-page-stack">
      <div className="card section-card">
        <EmptyState title={title} desc={desc} />
      </div>
    </div>
  );
}
