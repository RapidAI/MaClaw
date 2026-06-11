import type { MouseEvent } from 'react';
import { BrowserOpenURL } from '../../../wailsjs/runtime';

export const MarkdownLink = ({ node, ...props }: any) => (
    <a
        {...props}
        onClick={(e: MouseEvent) => {
            e.preventDefault();
            if (props.href) BrowserOpenURL(props.href);
        }}
        style={{ cursor: 'pointer', color: 'var(--theme-link-color, #2f5f98)', textDecoration: 'underline' }}
    />
);
