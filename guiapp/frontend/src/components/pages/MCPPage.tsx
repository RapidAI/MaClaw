import type { ComponentProps } from 'react';
import { MCPManagementPanel } from '../remote/MCPManagementPanel';

type MCPPageProps = ComponentProps<typeof MCPManagementPanel>;

export const MCPPage = (props: MCPPageProps) => (
    <div className="secondary-page-shell mcp-page" style={{ padding: '10px', height: '100%', minHeight: 0, textAlign: 'left' }}>
        <MCPManagementPanel {...props} />
    </div>
);
