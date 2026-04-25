type Props = {
  title: string;
  subtitle: string;
};

export function TopHeader({ title, subtitle }: Props) {
  return (
    <header className="center-top card">
      <div className="center-top-copy">
        <div className="mini light">Operating Console</div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
      <div className="center-top-chip">
        <strong>iWorkerCenter</strong>
        <span>AI Native Organization Hub</span>
      </div>
    </header>
  );
}
