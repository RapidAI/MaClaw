import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import { DialogProvider } from './components/CustomDialog'
import { ToastProvider } from './components/Toast'
import { KnowledgeImportProvider } from './components/settings/KnowledgeImportContext'
import { installWindowDragHandler } from './utils/windowDrag'

installWindowDragHandler()

function getStartupLang() {
    const lang = document.documentElement.lang || navigator.language || 'en'
    if (lang === 'zh-Hans') return 'zh-Hans'
    if (lang === 'zh-Hant') return 'zh-Hant'
    return 'en'
}

function localizeStartupText(en: string, zhHans: string, zhHant: string = zhHans) {
    const lang = getStartupLang()
    return lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
}

let startupErrorShown = false

function renderStartupError(error: unknown) {
    if (startupErrorShown) return  // idempotent — avoid double-render on re-entrant calls
    startupErrorShown = true
    const container = document.getElementById('root')
    if (!container) return
    const message = error instanceof Error ? `${error.name}: ${error.message}` : String(error)
    const errorView = (
        <div style={{ fontFamily: 'Arial, sans-serif', padding: 24, color: 'var(--theme-text-primary, #111827)', background: 'var(--theme-surface, #ffffff)', minHeight: '100%', boxSizing: 'border-box' }}>
            <h2 style={{ margin: '0 0 12px', color: 'var(--theme-danger, #dc2626)' }}>{localizeStartupText('Startup error', '启动错误', '啟動錯誤')}</h2>
            <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', background: 'var(--theme-surface-muted, #f9fafb)', border: '1px solid var(--theme-border, #e5e7eb)', borderRadius: 8, padding: 12 }}>{message}</pre>
        </div>
    )
    if (appRoot) {
        try {
            appRoot.render(errorView)
        } catch {
            // React root itself is corrupted — fall back to raw text.
            container.textContent = message
        }
        return
    }
    container.textContent = message
}

const container = document.getElementById('root')

if (!container) {
    throw new Error('Missing #root container')
}

let appRoot: ReturnType<typeof createRoot> | null = createRoot(container, {
    // React 18 reports errors that don't crash the tree here (e.g. hydration
    // mismatches, recoverable errors in Suspense boundaries).  Log them to
    // console rather than crashing.
    onRecoverableError(error, errorInfo) {
        console.error('[react] Recoverable error:', error, errorInfo)
    },
})

// Tracks whether the initial render() call completed without synchronous
// failure.  Note: React 18 createRoot.render() is asynchronous — component-
// level errors propagate via reportError / window.error, not as a synchronous
// throw from render().  This flag is set immediately after render() returns,
// meaning it guards only against infrastructure-level failures (missing
// container, broken JSX transform, etc.).
let renderScheduled = false

try {
    console.log('[startup-trace] main.tsx: scheduling React render')
    appRoot.render(
        <React.StrictMode>
            <ToastProvider>
                <KnowledgeImportProvider>
                    <DialogProvider>
                        <App/>
                    </DialogProvider>
                </KnowledgeImportProvider>
            </ToastProvider>
        </React.StrictMode>
    )
    renderScheduled = true
    console.log('[startup-trace] main.tsx: render scheduled OK')
} catch (error) {
    console.error('Failed to schedule initial render', error)
    renderStartupError(error)
}

window.addEventListener('error', (event) => {
    if (!renderScheduled) {
        // Render was never scheduled — show the startup error page.
        console.error('[startup] Unhandled error before render:', event.error || event.message)
        renderStartupError(event.error || event.message)
    } else {
        // App is running.  Do NOT replace the React tree — that would destroy
        // all context providers (DialogProvider, ToastProvider, etc.) and cause
        // cascading "must be used within Provider" errors + white screen.
        // React's own Suspense/ErrorBoundary propagation handles component
        // failures; we just log here for diagnostics.
        console.error('[runtime] Unhandled error:', event.error || event.message)
    }
})

window.addEventListener('unhandledrejection', (event) => {
    // Catches lazy-chunk import() failures and async errors.
    // Same principle: never nuke the React tree after it's running.
    if (!renderScheduled) {
        console.error('[startup] Unhandled rejection before render:', event.reason)
        renderStartupError(event.reason)
    } else {
        console.error('[runtime] Unhandled rejection:', event.reason)
    }
})
