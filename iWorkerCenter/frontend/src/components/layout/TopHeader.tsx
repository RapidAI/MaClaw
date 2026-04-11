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
      <div className="center-top-actions">
        <span className="badge ok">已连接</span>
        <button type="button" className="ghost">刷新数据</button>
      </div>
    </header>
  );
}
