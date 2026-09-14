export function cssColor(el: HTMLElement): string {
    return (el.style.color || "").replace(/\s+/g, "");
}

export function cssPaint(value: string): string {
    return value.replace(/\s+/g, "");
}

export function hexToRgb(hex: string): string {
    const n = parseInt(hex.slice(1), 16);
    return `rgb(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255})`;
}
