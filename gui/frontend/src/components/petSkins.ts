import clawMateSrc from '../assets/images/maclaw-clawmate.svg';
import devClawSrc from '../assets/images/maclaw-dev-claw.svg';
import focusClawSrc from '../assets/images/maclaw-focus-claw.svg';
import miniClawSrc from '../assets/images/maclaw-mini-claw.svg';

export type PetSkinId = 'clawmate' | 'mini-claw' | 'dev-claw' | 'focus-claw';

export interface PetSkinOption {
    id: PetSkinId;
    label: string;
    alt: string;
    tone: string;
    desc: string;
    previewLine: string;
    image: string;
}

export const defaultPetSkinId: PetSkinId = 'clawmate';
export const defaultPetSize = 88;

export const petSkinOptions: PetSkinOption[] = [
    {
        id: 'clawmate',
        label: 'ClawMate',
        alt: 'MaClaw ClawMate',
        tone: 'Balanced',
        desc: 'Default MaClaw claw companion',
        previewLine: 'Catches the problem and pulls out the signal.',
        image: clawMateSrc,
    },
    {
        id: 'mini-claw',
        label: 'Mini Claw',
        alt: 'MaClaw Mini Claw',
        tone: 'Compact',
        desc: 'Minimal desktop pet companion',
        previewLine: 'Small, fast, and easy to keep near the edge.',
        image: miniClawSrc,
    },
    {
        id: 'dev-claw',
        label: 'Dev Claw',
        alt: 'MaClaw Dev Claw',
        tone: 'Developer',
        desc: 'Developer-focused companion style',
        previewLine: 'More technical, direct, and ready for coding turns.',
        image: devClawSrc,
    },
    {
        id: 'focus-claw',
        label: 'Focus Claw',
        alt: 'MaClaw Focus Claw',
        tone: 'Focus',
        desc: 'Quiet low-distraction desktop presence',
        previewLine: 'Calmer motion for long focus sessions.',
        image: focusClawSrc,
    },
];

export function getPetSkinOption(id: unknown): PetSkinOption {
    if (typeof id === 'string') {
        const skin = petSkinOptions.find((option) => option.id === id);
        if (skin) return skin;
    }
    return petSkinOptions.find((option) => option.id === defaultPetSkinId) || petSkinOptions[0];
}

export function normalizePetSkinId(id: unknown): PetSkinId {
    return getPetSkinOption(id).id;
}
