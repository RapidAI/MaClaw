import { useEffect, useRef, useState } from 'react';
import { defaultCollabApi, FEATURES_OFF, type Assignment, type CollabApi, type CollabFeatures, type SaidItem } from './botCollab';

export const COLLAB_POLL_MS = 5000;

/**
 * Polls the Hub for the collaboration parts that are on: what bots said to
 * the person and the assignment board. Features are read first; a part
 * that is off is never polled and its screens stay hidden.
 */
export function useBotCollab(active: boolean, api: CollabApi = defaultCollabApi, pollMs = COLLAB_POLL_MS) {
    const [features, setFeatures] = useState<CollabFeatures>(FEATURES_OFF);
    const [said, setSaid] = useState<SaidItem[]>([]);
    const [assignments, setAssignments] = useState<Assignment[]>([]);
    const featuresRef = useRef(features);
    featuresRef.current = features;

    useEffect(() => {
        if (!active) return;
        let live = true;
        let ticks = 0;
        const tick = async () => {
            try {
                if (ticks++ % 12 === 0) {
                    const f = await api.features();
                    if (!live) return;
                    featuresRef.current = f;
                    setFeatures(f);
                }
                const f = featuresRef.current;
                const [s, a] = await Promise.all([
                    f.say ? api.said('') : Promise.resolve([] as SaidItem[]),
                    f.assignments ? api.assignments() : Promise.resolve([] as Assignment[]),
                ]);
                if (!live) return;
                setSaid(prev => (sameIds(prev, s || []) ? prev : s || []));
                setAssignments(a || []);
            } catch {
                // An older Hub has no such routes: keep everything hidden.
            }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, pollMs);
        return () => { live = false; window.clearInterval(timer); };
    }, [active, api, pollMs]);

    return { features, said, assignments };
}

function sameIds(a: SaidItem[], b: SaidItem[]): boolean {
    return a.length === b.length && a.every((x, i) => x.id === b[i].id);
}
