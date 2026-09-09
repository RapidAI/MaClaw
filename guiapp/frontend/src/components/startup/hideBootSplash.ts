export const BOOT_SPLASH_ID = 'maclaw-boot-splash';

/** Remove the HTML boot splash that covers the WebView before React paints. */
export function hideBootSplash(): void {
    const el = document.getElementById(BOOT_SPLASH_ID);
    if (el) el.remove();
}
