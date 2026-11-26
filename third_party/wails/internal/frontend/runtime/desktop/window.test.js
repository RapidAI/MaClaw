import { expect, describe, it, afterEach, vi } from 'vitest'
import { WindowFullscreen } from './window'

afterEach(() => {
    delete window.WailsInvoke
    delete window.wails
})

describe('WindowFullscreen', () => {
    it('suspends frameless resize tracking before invoking native fullscreen', () => {
        const order = []
        window.wails = {
            suspendResizeCursor: vi.fn(() => order.push('suspend')),
            flags: { enableResize: true },
        }
        window.WailsInvoke = vi.fn(() => order.push('invoke'))

        WindowFullscreen()

        expect(order).toEqual(['suspend', 'invoke'])
        expect(window.wails.suspendResizeCursor).toBeCalledTimes(1)
        expect(window.WailsInvoke).toHaveBeenLastCalledWith('WF')
    })

    it('falls back to disabling resize tracking when the suspend helper is absent', () => {
        window.wails = { flags: { enableResize: true } }
        window.WailsInvoke = vi.fn(() => {})

        WindowFullscreen()

        expect(window.wails.flags.enableResize).toBe(false)
        expect(window.WailsInvoke).toHaveBeenLastCalledWith('WF')
    })

    it('still invokes native fullscreen without resize state', () => {
        window.WailsInvoke = vi.fn(() => {})

        WindowFullscreen()

        expect(window.WailsInvoke).toHaveBeenLastCalledWith('WF')
    })
})
