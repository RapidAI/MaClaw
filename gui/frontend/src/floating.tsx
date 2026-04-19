/**
 * floating.tsx — Entry point for the floating assistant button window.
 *
 * This is a separate, minimal entry point that renders ONLY the FloatingButton
 * component. It does not load the full React app (App.tsx), keeping the bundle
 * size small for the lightweight floating window.
 *
 * Requirements: 11.1, 11.2
 */

import React from 'react';
import { createRoot } from 'react-dom/client';
import { FloatingButton } from './components/FloatingButton';

const container = document.getElementById('root');

if (!container) {
    throw new Error('Missing #root container in floating.html');
}

const root = createRoot(container);

root.render(
    <React.StrictMode>
        <FloatingButton />
    </React.StrictMode>
);
