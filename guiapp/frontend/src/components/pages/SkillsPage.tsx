import type { ComponentProps } from 'react';
import { SkillsManagementPanel } from '../remote/SkillsManagementPanel';

type SkillsPageProps = ComponentProps<typeof SkillsManagementPanel>;

export const SkillsPage = (props: SkillsPageProps) => (
    <div className="secondary-page-shell skills-page" style={{ display: 'flex', flex: '1 1 auto', flexDirection: 'column', minHeight: 0, overflow: 'hidden', textAlign: 'left' }}>
        <SkillsManagementPanel {...props} />
    </div>
);
