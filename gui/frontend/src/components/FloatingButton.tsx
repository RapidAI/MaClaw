/**
 * FloatingButton.tsx — Floating assistant button component (rendered in the second window).
 *
 * Renders a 56×56px circular button with the maclaw logo and a pulsing halo animation.
 * Supports:
 *   - Left-click → calls OnFloatingButtonClicked() via Wails binding
 *   - Drag (displacement > 5px) → calls OnFloatingButtonDragged(x, y) with rAF throttling
 *   - Right-click → custom context menu with "隐藏" (Hide) option
 *
 * Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 3.1, 3.2, 4.1, 4.2, 4.3, 4.4, 11.3
 */

import { useState, useRef, useCallback, useEffect } from 'react';
import './FloatingButton.css';
import logoSrc from '../assets/images/maclaw2.png';

// ── Wails Go binding bridge ─────────────────────────────────────────────────
// The floating window's WebView will have Wails bindings injected.
// We declare the expected shape and provide safe fallbacks so the component
// renders correctly even before the Go backend is wired up.

declare global {
    interface Window {
        go?: {
            main?: {
                App?: {
                    OnFloatingButtonClicked(): Promise<void>;
                    OnFloatingButtonDragged(x: number, y: number): Promise<void>;
                    HideFloatingButton(): Promise<void>;
                    QuitApp(): Promise<void>;
                };
            };
        };
    }
}

function callGoBinding(method: 'OnFloatingButtonClicked'): void;
function callGoBinding(method: 'OnFloatingButtonDragged', x: number, y: number): void;
function callGoBinding(method: 'HideFloatingButton'): void;
function callGoBinding(method: 'QuitApp'): void;
function callGoBinding(method: string, ...args: unknown[]): void {
    try {
        const app = window.go?.main?.App;
        if (app && typeof (app as Record<string, unknown>)[method] === 'function') {
            (app as Record<string, (...a: unknown[]) => unknown>)[method](...args);
        } else {
            console.warn(`[FloatingButton] Go binding not available: ${method}`);
        }
    } catch (err) {
        console.error(`[FloatingButton] Error calling ${method}:`, err);
    }
}

// ── Drag threshold (pixels) ─────────────────────────────────────────────────
const DRAG_THRESHOLD = 5;

// ── Component ───────────────────────────────────────────────────────────────

export function FloatingButton() {
    const [showMenu, setShowMenu] = useState(false);
    const [menuPos, setMenuPos] = useState({ x: 0, y: 0 });

    // Drag state tracked via ref to avoid re-renders during drag.
    const dragRef = useRef({
        startScreenX: 0,
        startScreenY: 0,
        isDragging: false,
        rafId: 0,
    });

    // ── Left-click / Drag handling ──────────────────────────────────────────

    const handleMouseDown = useCallback((e: React.MouseEvent) => {
        if (e.button !== 0) return; // only left button
        e.preventDefault();

        // Close context menu if open.
        setShowMenu(false);

        const drag = dragRef.current;
        drag.startScreenX = e.screenX;
        drag.startScreenY = e.screenY;
        drag.isDragging = false;

        const onMouseMove = (ev: MouseEvent) => {
            const dx = ev.screenX - drag.startScreenX;
            const dy = ev.screenY - drag.startScreenY;

            if (!drag.isDragging) {
                // Check if displacement exceeds threshold.
                if (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD) {
                    drag.isDragging = true;
                } else {
                    return;
                }
            }

            // Throttle window move calls with requestAnimationFrame.
            if (drag.rafId) return;
            drag.rafId = requestAnimationFrame(() => {
                drag.rafId = 0;
                // Compute new window position: current window position + delta.
                // window.screenX/screenY give the window's position on screen.
                const newX = window.screenX + (ev.screenX - drag.startScreenX);
                const newY = window.screenY + (ev.screenY - drag.startScreenY);
                drag.startScreenX = ev.screenX;
                drag.startScreenY = ev.screenY;
                callGoBinding('OnFloatingButtonDragged', newX, newY);
            });
        };

        const onMouseUp = () => {
            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);

            if (drag.rafId) {
                cancelAnimationFrame(drag.rafId);
                drag.rafId = 0;
            }

            if (!drag.isDragging) {
                // Displacement ≤ threshold → treat as left-click.
                callGoBinding('OnFloatingButtonClicked');
            }
        };

        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    }, []);

    // ── Right-click context menu ────────────────────────────────────────────

    const handleContextMenu = useCallback((e: React.MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setMenuPos({ x: e.clientX, y: e.clientY });
        setShowMenu(true);
    }, []);

    const handleHide = useCallback(() => {
        setShowMenu(false);
        callGoBinding('HideFloatingButton');
    }, []);

    const handleQuit = useCallback(() => {
        setShowMenu(false);
        callGoBinding('QuitApp');
    }, []);

    // Close context menu when clicking outside.
    useEffect(() => {
        if (!showMenu) return;

        const handleClickOutside = (e: MouseEvent) => {
            // If the click is not on a menu item, close the menu.
            const target = e.target as HTMLElement;
            if (!target.closest('.floating-context-menu')) {
                setShowMenu(false);
            }
        };

        // Use a short delay so the current right-click event doesn't
        // immediately close the menu.
        const timer = setTimeout(() => {
            document.addEventListener('mousedown', handleClickOutside);
        }, 0);

        return () => {
            clearTimeout(timer);
            document.removeEventListener('mousedown', handleClickOutside);
        };
    }, [showMenu]);

    return (
        <div
            className="floating-assistant-container"
            onMouseDown={handleMouseDown}
            onContextMenu={handleContextMenu}
        >
            <div className="floating-assistant-halo" />
            <img
                src={logoSrc}
                className="floating-assistant-logo"
                alt="MaClaw Assistant"
                draggable={false}
            />
            {showMenu && (
                <div
                    className="floating-context-menu"
                    style={{ top: menuPos.y, left: menuPos.x }}
                >
                    <div
                        className="floating-context-menu-item"
                        onClick={handleHide}
                    >
                        隐藏
                    </div>
                    <div className="floating-context-menu-separator" />
                    <div
                        className="floating-context-menu-item"
                        onClick={handleQuit}
                    >
                        退出
                    </div>
                </div>
            )}
        </div>
    );
}

export default FloatingButton;
