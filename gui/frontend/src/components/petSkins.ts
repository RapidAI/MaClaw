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
        alt: 'MaClaw ClawMate workbench companion',
        tone: 'Balanced',
        desc: 'Workbench companion with ears, paws, and a signal tag',
        previewLine: 'A concrete helper that keeps tasks visible without feeling toy-like.',
        image: clawMateSrc,
    },
    {
        id: 'mini-claw',
        label: 'Mini Claw',
        alt: 'MaClaw Mini Claw pocket companion',
        tone: 'Compact',
        desc: 'Pocket-sized helper with a compact shell and tiny boots',
        previewLine: 'Small, fast, and easy to keep near the edge.',
        image: miniClawSrc,
    },
    {
        id: 'dev-claw',
        label: 'Dev Claw',
        alt: 'MaClaw Dev Claw coding companion',
        tone: 'Developer',
        desc: 'Coding companion with visor, terminal chest, and tool marks',
        previewLine: 'More technical, direct, and ready for coding turns.',
        image: devClawSrc,
    },
    {
        id: 'focus-claw',
        label: 'Focus Claw',
        alt: 'MaClaw Focus Claw quiet companion',
        tone: 'Focus',
        desc: 'Quiet companion with soft eyes and low-motion presence',
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
