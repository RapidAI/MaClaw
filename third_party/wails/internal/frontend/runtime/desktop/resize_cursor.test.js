import { expect, describe, it } from 'vitest'
import { isResizeCursor, resizeCursorForPointer } from './resize_cursor'

const border = 6

function at(overrides) {
    const metrics = {
        outerWidth: 800,
        outerHeight: 600,
        innerWidth: 800,
        innerHeight: 600,
        clientX: 400,
        clientY: 300,
        border,
        ...overrides,
    }
    return resizeCursorForPointer(
        metrics.outerWidth,
        metrics.outerHeight,
        metrics.innerWidth,
        metrics.innerHeight,
        metrics.clientX,
        metrics.clientY,
        metrics.border,
    )
}

describe('resizeCursorForPointer', () => {
    it('leaves the middle of a normal window alone', () => {
        expect(at({})).toBe('')
    })

    it('names each edge and corner', () => {
        expect(at({ clientX: 2, clientY: 300 })).toBe('w-resize')
        expect(at({ clientX: 400, clientY: 2 })).toBe('n-resize')
        expect(at({ clientX: 400, clientY: 596 })).toBe('s-resize')
        expect(at({ clientX: 796, clientY: 300 })).toBe('e-resize')
        expect(at({ clientX: 796, clientY: 596 })).toBe('se-resize')
        expect(at({ clientX: 2, clientY: 596 })).toBe('sw-resize')
        expect(at({ clientX: 2, clientY: 2 })).toBe('nw-resize')
        expect(at({ clientX: 796, clientY: 2 })).toBe('ne-resize')
    })

    it('does not invent a sizing cursor when maximise left the outer size behind', () => {
        expect(at({ outerWidth: 800, outerHeight: 600, innerWidth: 1920, innerHeight: 1040, clientX: 1900, clientY: 1000 })).toBe('')
        expect(at({ outerWidth: 800, outerHeight: 600, innerWidth: 1920, innerHeight: 1040, clientX: 790, clientY: 590 })).toBe('')
    })

    it('does not invent a sizing cursor when the outer size is zero or the pointer is past it', () => {
        expect(at({ outerWidth: 0, outerHeight: 0, innerWidth: 1920, innerHeight: 1080, clientX: 10, clientY: 10 })).toBe('')
        expect(at({ clientX: 900, clientY: 300 })).toBe('')
        expect(at({ clientX: 400, clientY: 700 })).toBe('')
    })

    it('still resizes when outer and inner disagree by a few pixels', () => {
        expect(at({ outerWidth: 799, innerWidth: 800, clientX: 796 })).toBe('e-resize')
        expect(at({ outerWidth: 780, innerWidth: 800, clientX: 778 })).toBe('e-resize')
    })

    it('drops the sizing cursor once the page is far larger than the reported window', () => {
        expect(at({ outerWidth: 760, innerWidth: 800, clientX: 790 })).toBe('')
    })
})

describe('isResizeCursor', () => {
    it('recognises only sizing cursors', () => {
        expect(isResizeCursor('se-resize')).toBe(true)
        expect(isResizeCursor('text')).toBe(false)
        expect(isResizeCursor('')).toBe(false)
        expect(isResizeCursor(null)).toBe(false)
    })
})
