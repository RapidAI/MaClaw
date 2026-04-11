type Props = {
  label: string;
  value: string;
  hint: string;
};

export function MetricCard({ label, value, hint }: Props) {
  return (
    <div className="metric-card card soft">
      <label>{label}</label>
      <strong>{value}</strong>
      <span>{hint}</span>
    </div>
  );
}
