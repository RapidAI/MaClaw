type Props = {
  title: string;
  desc: string;
};

export function EmptyState({ title, desc }: Props) {
  return (
    <div className="empty-state">
      <strong>{title}</strong>
      <p>{desc}</p>
    </div>
  );
}
