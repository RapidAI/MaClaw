import type { ComponentProps } from 'react';
import { SkillsManagementPanel } from '../remote/SkillsManagementPanel';

type SkillsPageProps = ComponentProps<typeof SkillsManagementPanel>;

export const SkillsPage = (props: SkillsPageProps) => (
    <div style={{ padding: '10px', height: '100%', minHeight: 0, textAlign: 'left' }}>
        <SkillsManagementPanel {...props} />
    </div>
);
