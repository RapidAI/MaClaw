import clawMateSrc from '../assets/images/maclaw-clawmate.svg';

/** Built-in official ids remain known for typing; runtime packs may add more. */
export type PetSkinId = string;

export interface PetSkinOption {
    id: PetSkinId;
    label: string;
    alt: string;
    tone: string;
    desc: string;
    /** Original `description` from pet-pack.yaml, retained for publishing. */
    manifestDescription?: string;
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
    renderer?: string;
}

export const defaultPetSkinId: PetSkinId = 'clawmate';
export const defaultPetSize = 88;

const builtinImages: Record<string, string> = {
    'clawmate': clawMateSrc,
};

export const petSkinOptions: PetSkinOption[] = [
    {
        id: 'clawmate',
        label: 'ClawMate',
        alt: 'MaClaw ClawMate mechanical crab companion',
        tone: 'Balanced',
        desc: 'A calm mechanical crab companion with expressive eyes and compact claws',
        previewLine: 'The official animated reference; add your own character through a pet pack.',
        image: clawMateSrc,
        variants: ['default'],
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
            variants: ['default'],
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
    const variants = Array.isArray(pack.variants) ? (pack.variants as string[]) : ['default'];
    const scope = String(pack.scope || '');
    return {
        id,
        label,
        alt: label,
        tone: String(pack.tone || 'balanced'),
        desc,
        manifestDescription: plainDesc || desc,
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
        renderer: String(pack.renderer || ''),
    };
}

export function isBuiltinPetSkinImage(id: string): boolean {
    return Object.prototype.hasOwnProperty.call(builtinImages, id);
}
