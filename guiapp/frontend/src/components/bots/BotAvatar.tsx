import type { BotShape } from './botAppearance';

// Body outlines in a 32x32 box. Each keeps the face area (y 12..18) inside.
const PATHS: Record<BotShape, string> = {
    blob: 'M16 2.5c7.6 0 13.5 4.6 13.5 12.6S24.4 29.5 16 29.5 2.5 24.7 2.5 15.6 8.4 2.5 16 2.5z',
    cloud: 'M9 27.5a6.5 6.5 0 0 1-1.2-12.9A8.5 8.5 0 0 1 24 11.6a7.6 7.6 0 0 1 .6 15.9z',
    leaf: 'M16 2.5c5 5.5 13.5 13 13.5 18.2A8.8 8.8 0 0 1 20.7 29.5H11.3a8.8 8.8 0 0 1-8.8-8.8C2.5 15.5 11 8 16 2.5z',
    square: 'M9 3h14a6 6 0 0 1 6 6v14a6 6 0 0 1-6 6H9a6 6 0 0 1-6-6V9a6 6 0 0 1 6-6z',
    pill: 'M10 6.5h12a9.5 9.5 0 0 1 0 19H10a9.5 9.5 0 0 1 0-19z',
    circle: 'M16 3a13 13 0 1 1 0 26 13 13 0 0 1 0-26z',
};

type Props = {
    color: string;
    shape: BotShape;
    className?: string;
    title?: string;
    testId?: string;
};

/** A colored badge body with two white eyes. Decorative unless title is set. */
export function BotAvatar({ color, shape, className, title, testId }: Props) {
    const eyeY = shape === 'cloud' ? 19 : shape === 'leaf' ? 19.5 : 16;
    return (
        <span
            className={'bot-avatar ' + (className || '')}
            data-shape={shape}
            data-color={color}
            data-testid={testId}
            role={title ? 'img' : undefined}
            aria-label={title}
            aria-hidden={title ? undefined : true}
        >
            <svg viewBox="0 0 32 32" width="100%" height="100%" aria-hidden="true">
                <path d={PATHS[shape] || PATHS.circle} fill={color} />
                <circle cx="12.5" cy={eyeY} r="2" fill="#fff" />
                <circle cx="19.5" cy={eyeY} r="2" fill="#fff" />
            </svg>
        </span>
    );
}
