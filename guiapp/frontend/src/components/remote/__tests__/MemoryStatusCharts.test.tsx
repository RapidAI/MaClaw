import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PieChart } from '../MemoryStatusCharts';

describe('MemoryStatusCharts', () => {
    it('draws a complete donut when one category occupies 100 percent', () => {
        const { container } = render(<PieChart
            data={[{ category: 'user_fact', label: 'User facts', count: 4, percent: 100 }]}
            centerLabel="entries"
            ariaLabel="Memory categories"
        />);
        const path = container.querySelector('path');
        expect(path).not.toBeNull();
        const definition = path?.getAttribute('d') || '';
        expect(definition.match(/ A /g)?.length).toBe(4);
        expect(container.textContent).toContain('4');
    });
});
