// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { BOOT_SPLASH_ID, hideBootSplash } from './hideBootSplash';

afterEach(() => {
    document.getElementById(BOOT_SPLASH_ID)?.remove();
});

describe('hideBootSplash', () => {
    it('removes the HTML boot splash when it is still mounted', () => {
        const el = document.createElement('div');
        el.id = BOOT_SPLASH_ID;
        document.body.appendChild(el);
        hideBootSplash();
        expect(document.getElementById(BOOT_SPLASH_ID)).toBeNull();
    });

    it('is a no-op when the boot splash is already gone', () => {
        expect(() => hideBootSplash()).not.toThrow();
        expect(document.getElementById(BOOT_SPLASH_ID)).toBeNull();
    });
});
