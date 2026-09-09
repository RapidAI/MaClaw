export type TTSVoiceOption = {
    id: string;
    label: string;
    zh: string;
    welcomeZh: string;
    welcomeZhHant: string;
    welcomeEn: string;
};

// Keep this list aligned with corelib/tts.SupportedTTSVoiceIDs. The bundled
// Kokoro voice pack contains exactly these voices.
export const ttsVoiceOptions: readonly TTSVoiceOption[] = [
    { id: 'zf_xiaoyi', label: 'zf_xiaoyi', zh: '小艺，中文女声，默认', welcomeZh: '小艺（中文女声）', welcomeZhHant: '小藝（中文女聲）', welcomeEn: 'Xiaoyi (Chinese female)' },
    { id: 'zf_xiaoxiao', label: 'zf_xiaoxiao', zh: '晓晓，中文女声', welcomeZh: '晓晓（中文女声）', welcomeZhHant: '曉曉（中文女聲）', welcomeEn: 'Xiaoxiao (Chinese female)' },
    { id: 'zm_yunxi', label: 'zm_yunxi', zh: '云希，中文男声', welcomeZh: '云希（中文男声）', welcomeZhHant: '雲希（中文男聲）', welcomeEn: 'Yunxi (Chinese male)' },
    { id: 'zm_yunyang', label: 'zm_yunyang', zh: '云扬，中文男声', welcomeZh: '云扬（中文男声）', welcomeZhHant: '雲揚（中文男聲）', welcomeEn: 'Yunyang (Chinese male)' },
    { id: 'am_adam', label: 'Adam · American English', zh: 'Adam · 美式英语男声', welcomeZh: '自然男声（美式英语）', welcomeZhHant: '自然男聲（美式英語）', welcomeEn: 'Natural male (American English)' },
    { id: 'af_heart', label: 'Heart · Sweet American English', zh: 'Heart · 甜美美式英语女声', welcomeZh: '甜美女声（美式英语）', welcomeZhHant: '甜美女聲（美式英語）', welcomeEn: 'Sweet female (American English)' },
];
