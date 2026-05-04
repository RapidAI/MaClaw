import type { ComponentProps } from 'react';
import { GossipPanel } from '../gossip/GossipPanel';

type GossipPageProps = ComponentProps<typeof GossipPanel>;

export const GossipPage = (props: GossipPageProps) => <GossipPanel {...props} />;
