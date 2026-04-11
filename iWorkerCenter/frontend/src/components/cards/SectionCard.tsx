import type { ReactNode } from 'react';

type Props = {
  title: string;
  desc?: string;
  children: ReactNode;
};

export function SectionCard({ title, desc, children }: Props) {
  return (
    <section className="card section-card">
      <div className="section-head">
        <div>
          <h3>{title}</h3>
          {desc ? <p>{desc}</p> : null}
        </div>
      </div>
      {children}
    </section>
  );
}
