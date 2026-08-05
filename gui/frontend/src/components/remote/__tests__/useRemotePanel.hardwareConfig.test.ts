import { describe, expect, it } from 'vitest';
import { supportsAtomicRemoteConfigPatch } from '../useRemotePanel';

describe('hardware remote config patches', () => {
    it.each([
        'hardware_welcome_enabled',
        'hardware_welcome_text',
        'hardware_welcome_voice_id',
        'hardware_welcome_audio_path',
        'hardware_volume',
        'pet_ambient_city',
    ])('allows %s through the atomic persistence path', (field) => {
        expect(supportsAtomicRemoteConfigPatch({ [field]: true })).toBe(true);
    });
});
