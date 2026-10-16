import { useRef, type MouseEventHandler } from "react";

type SafeBackdropDismissOptions = {
    enabled?: boolean;
};

export function useSafeBackdropDismiss<T extends HTMLElement = HTMLDivElement>(
    onDismiss: () => void | Promise<void>,
    options: SafeBackdropDismissOptions = {},
) {
    const mouseDownStartedOnBackdropRef = useRef(false);
    const enabled = options.enabled ?? true;

    const backdropProps = {
        onMouseDown: ((event) => {
            mouseDownStartedOnBackdropRef.current = enabled && event.target === event.currentTarget;
        }) as MouseEventHandler<T>,
        onClick: ((event) => {
            if (enabled && event.target === event.currentTarget && mouseDownStartedOnBackdropRef.current) {
                void onDismiss();
            }
            mouseDownStartedOnBackdropRef.current = false;
        }) as MouseEventHandler<T>,
    };

    const markPressInside = () => {
        mouseDownStartedOnBackdropRef.current = false;
    };
    const dialogProps = {
        onMouseDown: ((event) => {
            event.stopPropagation();
            markPressInside();
        }) as MouseEventHandler<T>,
        // Selecting the password can start on the dimmed backdrop, cross the
        // field, and end on the backdrop again. Both ends are the backdrop, so
        // the click would close the dialog. Entering the dialog cancels that.
        onMouseEnter: (() => {
            markPressInside();
        }) as MouseEventHandler<T>,
        onPointerEnter: (() => {
            markPressInside();
        }) as MouseEventHandler<T>,
        // Selecting text can start on the dimmed backdrop and end inside the
        // field. The click is still dispatched on the backdrop; clearing the
        // press here keeps that selection from closing the dialog.
        onMouseUp: (() => {
            markPressInside();
        }) as MouseEventHandler<T>,
        onPointerUp: (() => {
            markPressInside();
        }) as MouseEventHandler<T>,
        onClick: ((event) => {
            event.stopPropagation();
        }) as MouseEventHandler<T>,
    };

    return { backdropProps, dialogProps };
}
