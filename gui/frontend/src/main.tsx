import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import { DialogProvider } from './components/CustomDialog'
import { ToastProvider } from './components/Toast'

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

function renderStartupError(error: unknown) {
    const container = document.getElementById('root')
    if (!container) return
    const message = error instanceof Error ? `${error.name}: ${error.message}` : String(error)
    const errorView = (
        <div style={{ fontFamily: 'Arial, sans-serif', padding: 24, color: 'var(--theme-text-primary, #111827)', background: 'var(--theme-surface, #ffffff)', minHeight: '100vh', boxSizing: 'border-box' }}>
            <h2 style={{ margin: '0 0 12px', color: 'var(--theme-danger, #dc2626)' }}>{localizeStartupText('Startup error', '启动错误', '啟動錯誤')}</h2>
            <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', background: 'var(--theme-surface-muted, #f9fafb)', border: '1px solid var(--theme-border, #e5e7eb)', borderRadius: 8, padding: 12 }}>{message}</pre>
        </div>
    )
    if (appRoot) {
        appRoot.render(errorView)
        return
    }
    container.textContent = message
}

const container = document.getElementById('root')

if (!container) {
    throw new Error('Missing #root container')
}

let appRoot: ReturnType<typeof createRoot> | null = createRoot(container)

try {
    appRoot.render(
        <React.StrictMode>
            <ToastProvider>
                <DialogProvider>
                    <App/>
                </DialogProvider>
            </ToastProvider>
        </React.StrictMode>
    )
} catch (error) {
    console.error('Failed to render app', error)
    renderStartupError(error)
}

window.addEventListener('error', (event) => {
    console.error('Unhandled startup error', event.error || event.message)
    renderStartupError(event.error || event.message)
})
