import type { ComponentProps } from 'react';
import { RemoteSessionList } from '../remote/RemoteSessionList';

type RemoteSessionsPageProps = ComponentProps<typeof RemoteSessionList>;

export const RemoteSessionsPage = (props: RemoteSessionsPageProps) => (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        <div style={{ flex: 1, overflowY: 'auto', padding: '20px', overflowX: 'hidden' }}>
            <RemoteSessionList {...props} />
        </div>
    </div>
);
