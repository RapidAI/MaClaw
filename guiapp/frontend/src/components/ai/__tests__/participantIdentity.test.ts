import { describe, expect, it } from 'vitest';
import { participantIdentityKeys, participantIdentityMatches } from '../participantIdentity';

describe('participantIdentity', () => {
    it('treats machine and generated virtual employee aliases as equivalent', () => {
        expect(participantIdentityMatches('machine-1', 've_machine-1')).toBe(true);
        expect(participantIdentityMatches('machine-1', 've-machine-1')).toBe(true);
        expect(participantIdentityMatches('ve_machine-1', 've-machine-1')).toBe(true);
    });

    it('expands identity keys for raw, underscore, and dash virtual employee ids', () => {
        expect(new Set(participantIdentityKeys('ve_machine-1'))).toEqual(new Set([
            'machine_1',
            've_machine_1',
            've-machine_1',
        ]));
    });
});
