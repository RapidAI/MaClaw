import type { ComponentProps } from 'react';
import { MCPManagementPanel } from '../remote/MCPManagementPanel';

type MCPPageProps = ComponentProps<typeof MCPManagementPanel>;

export const MCPPage = (props: MCPPageProps) => (
    <div style={{ padding: '10px', height: '100%', minHeight: 0, textAlign: 'left' }}>
        <MCPManagementPanel {...props} />
    </div>
);
