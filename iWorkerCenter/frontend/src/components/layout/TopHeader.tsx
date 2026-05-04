type Props = {
  title: string;
  subtitle: string;
  tenantId?: string;
  bootstrapReady?: boolean;
};

export function TopHeader({ title, subtitle, tenantId, bootstrapReady }: Props) {
  return (
    <header className="center-top card">
      <div>
        <div className="mini light">Admin Console</div>
        <h2>{title}</h2>
        <p>{subtitle}</p>
      </div>
      <div className="center-top-actions">
        {tenantId ? <span className="badge info">租户 {tenantId}</span> : null}
        <span className={bootstrapReady ? 'badge ok' : 'badge warn'}>{bootstrapReady ? '已初始化' : '待初始化'}</span>
        <button type="button" className="ghost" onClick={() => window.location.reload()}>刷新数据</button>
      </div>
    </header>
  );
}
