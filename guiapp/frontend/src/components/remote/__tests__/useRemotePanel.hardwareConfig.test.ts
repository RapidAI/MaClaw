import { describe, expect, it } from 'vitest';
import { supportsAtomicRemoteConfigPatch } from '../useRemotePanel';

describe('atomic remote config patches', () => {
    it.each([
        'hardware_welcome_enabled',
        'hardware_welcome_text',
        'hardware_welcome_voice_id',
        'hardware_welcome_audio_path',
        'hardware_volume',
        'hardware_brightness',
        'pet_ambient_city',
    ])('allows %s through the atomic persistence path', (field) => {
        expect(supportsAtomicRemoteConfigPatch({ [field]: true })).toBe(true);
    });

    it('does not expose reply-cache policy through the global persistence path', () => {
        expect(supportsAtomicRemoteConfigPatch({ answer_cache: { enabled: true, ttl_days: 7 } })).toBe(false);
    });
});
