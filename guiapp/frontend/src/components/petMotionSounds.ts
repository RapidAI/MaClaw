export const motionSoundPresetOptionIds = ['classic', 'bubble', 'chime', 'synth', 'soft'] as const;

export type MotionSoundPreset = typeof motionSoundPresetOptionIds[number];

export function normalizeMotionSoundPreset(value: unknown): MotionSoundPreset {
    return motionSoundPresetOptionIds.includes(value as MotionSoundPreset)
        ? value as MotionSoundPreset
        : 'classic';
}
