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

    const dialogProps = {
        onMouseDown: ((event) => {
            event.stopPropagation();
            mouseDownStartedOnBackdropRef.current = false;
        }) as MouseEventHandler<T>,
        onClick: ((event) => {
            event.stopPropagation();
        }) as MouseEventHandler<T>,
    };

    return { backdropProps, dialogProps };
}
