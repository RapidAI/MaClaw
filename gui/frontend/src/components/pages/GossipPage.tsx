import type { ComponentProps } from 'react';
import { GossipPanel } from '../gossip/GossipPanel';

type GossipPageProps = ComponentProps<typeof GossipPanel>;

export const GossipPage = (props: GossipPageProps) => (
    <div className="secondary-page-shell gossip-page">
        <GossipPanel {...props} />
    </div>
);
