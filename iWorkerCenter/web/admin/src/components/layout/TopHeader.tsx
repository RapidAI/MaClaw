type Props = {
  title: string;
  subtitle: string;
};

export function TopHeader({ title, subtitle }: Props) {
  return (
    <header className="center-top card">
      <div>
        <div className="mini light">Admin Console</div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
    </header>
  );
}
