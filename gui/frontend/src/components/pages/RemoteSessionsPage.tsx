import type { ComponentProps } from 'react';
import { RemoteSessionList } from '../remote/RemoteSessionList';

type RemoteSessionsPageProps = ComponentProps<typeof RemoteSessionList>;

export const RemoteSessionsPage = (props: RemoteSessionsPageProps) => (
    <div className="remote-sessions-page" data-testid="remote-sessions-page">
        <div className="remote-sessions-page__scroll">
            <div className="remote-sessions-page__inner">
                <RemoteSessionList {...props} />
            </div>
        </div>
    </div>
);
