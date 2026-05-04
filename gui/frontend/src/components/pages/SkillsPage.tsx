import type { ComponentProps } from 'react';
import { SkillsManagementPanel } from '../remote/SkillsManagementPanel';

type SkillsPageProps = ComponentProps<typeof SkillsManagementPanel>;

export const SkillsPage = (props: SkillsPageProps) => (
    <div style={{ padding: '10px' }}>
        <SkillsManagementPanel {...props} />
    </div>
);
