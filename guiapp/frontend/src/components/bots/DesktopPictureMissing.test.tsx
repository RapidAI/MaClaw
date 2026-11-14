import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { DesktopPictureMissing } from './DesktopPictureMissing';

describe('DesktopPictureMissing', () => {
    it('shows a retry instead of an empty picture', () => {
        const onRetry = vi.fn();
        render(<DesktopPictureMissing label="图片没载入，重试" onRetry={onRetry} />);
        fireEvent.click(screen.getByTestId('desktop-picture-missing'));
        expect(onRetry).toHaveBeenCalledOnce();
        expect(screen.getByTestId('desktop-picture-missing').textContent).toContain('重试');
    });
});
