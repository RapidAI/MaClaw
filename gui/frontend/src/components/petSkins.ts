import clawMateSrc from '../assets/images/maclaw-clawmate.svg';
import devClawSrc from '../assets/images/maclaw-dev-claw.svg';
import focusClawSrc from '../assets/images/maclaw-focus-claw.svg';
import miniClawSrc from '../assets/images/maclaw-mini-claw.svg';

/** Built-in official ids remain known for typing; runtime packs may add more. */
export type PetSkinId = string;

export interface PetSkinOption {
    id: PetSkinId;
    label: string;
    alt: string;
    tone: string;
    desc: string;
    previewLine: string;
    image: string;
    variants?: string[];
    scope?: string;
    faceOverlay?: boolean;
    canUninstall?: boolean;
    source?: string;
    hasPreview?: boolean;
    status?: string;
    version?: string;
}

export const defaultPetSkinId: PetSkinId = 'clawmate';
export const defaultPetSize = 88;

const builtinImages: Record<string, string> = {
    'clawmate': clawMateSrc,
    'mini-claw': miniClawSrc,
    'dev-claw': devClawSrc,
    'focus-claw': focusClawSrc,
};

export const petSkinOptions: PetSkinOption[] = [
    {
        id: 'clawmate',
        label: 'ClawMate',
        alt: 'MaClaw ClawMate workbench companion',
        tone: 'Balanced',
        desc: 'Workbench companion with ears, paws, and a signal tag',
        previewLine: 'A concrete helper that keeps tasks visible without feeling toy-like.',
        image: clawMateSrc,
        variants: ['classic', 'default'],
    },
    {
        id: 'mini-claw',
        label: 'Mini Claw',
        alt: 'MaClaw Mini Claw pocket companion',
        tone: 'Compact',
        desc: 'Pocket-sized helper with a compact shell and tiny boots',
        previewLine: 'Small, fast, and easy to keep near the edge.',
        image: miniClawSrc,
        variants: ['classic', 'default'],
    },
    {
        id: 'dev-claw',
        label: 'Dev Claw',
        alt: 'MaClaw Dev Claw coding companion',
        tone: 'Developer',
        desc: 'Coding companion with visor, terminal chest, and tool marks',
        previewLine: 'More technical, direct, and ready for coding turns.',
        image: devClawSrc,
        variants: ['classic', 'default'],
    },
    {
        id: 'focus-claw',
        label: 'Focus Claw',
        alt: 'MaClaw Focus Claw quiet companion',
        tone: 'Focus',
        desc: 'Quiet companion with soft eyes and low-motion presence',
        previewLine: 'Calmer motion for long focus sessions.',
        image: focusClawSrc,
        variants: ['classic', 'default'],
    },
];

export function getPetSkinOption(id: unknown, catalog: PetSkinOption[] = petSkinOptions): PetSkinOption {
    if (typeof id === 'string') {
        const skin = catalog.find((option) => option.id === id);
        if (skin) return skin;
        // Unknown pack id: synthesize a card so third-party packs still render.
        return {
            id,
            label: id,
            alt: id,
            tone: 'Custom',
            desc: id,
            previewLine: id,
            image: builtinImages[id] || clawMateSrc,
            variants: ['classic', 'default'],
        };
    }
    return catalog.find((option) => option.id === defaultPetSkinId) || catalog[0];
}

export function normalizePetSkinId(id: unknown, catalog: PetSkinOption[] = petSkinOptions): PetSkinId {
    return getPetSkinOption(id, catalog).id;
}

/** Map registry pack info from Go into UI options (labels from manifest). */
function pickI18nMap(value: unknown): Record<string, string> {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
        const out: Record<string, string> = {};
        for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
            if (typeof v === 'string' && v.trim()) out[k] = v;
        }
        return out;
    }
    return {};
}

export function packInfoToSkinOption(pack: Record<string, unknown>, lang: string): PetSkinOption {
    const id = String(pack.id || '');
    const labelMap = pickI18nMap(pack.label);
    const descMap = pickI18nMap(pack.description_i18n ?? pack.description);
    const plainDesc = typeof pack.description === 'string' ? pack.description : '';
    const label =
        (lang.startsWith('zh') ? labelMap['zh-Hans'] || labelMap['zh-Hant'] : labelMap.en) ||
        String(pack.name || id);
    const desc =
        (lang.startsWith('zh') ? descMap['zh-Hans'] || descMap['zh-Hant'] : descMap.en) ||
        plainDesc ||
        String(pack.name || id);
    const variants = Array.isArray(pack.variants) ? (pack.variants as string[]) : ['classic', 'default'];
    const scope = String(pack.scope || '');
    return {
        id,
        label,
        alt: label,
        tone: String(pack.tone || 'balanced'),
        desc,
        previewLine: desc,
        image: builtinImages[id] || clawMateSrc,
        variants,
        scope,
        faceOverlay: !!pack.face_overlay,
        canUninstall: !!pack.can_uninstall || scope === 'user',
        source: String(pack.source || ''),
        hasPreview: !!pack.has_preview,
        status: String(pack.status || ''),
        version: String(pack.version || ''),
    };
}

export function isBuiltinPetSkinImage(id: string): boolean {
    return Object.prototype.hasOwnProperty.call(builtinImages, id);
}
