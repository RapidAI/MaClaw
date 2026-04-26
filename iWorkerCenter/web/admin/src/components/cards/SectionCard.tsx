import type { ReactNode } from 'react';

type Props = {
  title: string;
  desc?: string;
  children: ReactNode;
  className?: string;
};

export function SectionCard({ title, desc, children, className }: Props) {
  return (
    <section className={`card section-card ${className || ''}`.trim()}>
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
