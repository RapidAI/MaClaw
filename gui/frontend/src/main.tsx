import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import App from './App'
import { DialogProvider } from './components/CustomDialog'

function renderStartupError(error: unknown) {
    const container = document.getElementById('root')
    if (!container) return
    const message = error instanceof Error ? `${error.name}: ${error.message}` : String(error)
    container.innerHTML = `
        <div style="font-family: Arial, sans-serif; padding: 24px; color: #111827; background: #ffffff; min-height: 100vh; box-sizing: border-box;">
            <h2 style="margin: 0 0 12px; color: #dc2626;">MaClaw startup error</h2>
            <pre style="white-space: pre-wrap; word-break: break-word; background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 8px; padding: 12px;">${message}</pre>
        </div>
    `
}

const container = document.getElementById('root')

if (!container) {
    throw new Error('Missing #root container')
}

const root = createRoot(container)

try {
    root.render(
        <React.StrictMode>
            <DialogProvider>
                <App/>
            </DialogProvider>
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
