// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { sanitizeEmailHtml } from '../emailHtml';

describe('sanitizeEmailHtml', () => {
    it('removes script carriers and event handlers', () => {
        const { html } = sanitizeEmailHtml(
            '<p onclick="steal()">hi<script>alert(1)</script><iframe src="https://x"></iframe><object data="x"></object><embed src="y"></p>',
            false,
        );
        expect(html).not.toContain('script');
        expect(html).not.toContain('alert');
        expect(html).not.toContain('iframe');
        expect(html).not.toContain('embed');
        expect(html).not.toContain('onclick');
        expect(html).toContain('hi');
    });

    it('drops javascript URLs but keeps the link text', () => {
        const { html } = sanitizeEmailHtml('<a href="javascript:alert(1)">click</a>', false);
        expect(html).toContain('<a');
        expect(html).toContain('click');
        expect(html).not.toContain('javascript');
    });

    it('keeps safe links and drops relative ones', () => {
        const { html } = sanitizeEmailHtml(
            '<a href="https://ok.example/a">ok</a><a href="mailto:a@b.c">mail</a><a href="/relative">rel</a>',
            false,
        );
        expect(html).toContain('href="https://ok.example/a"');
        expect(html).toContain('href="mailto:a@b.c"');
        expect(html).not.toContain('/relative');
    });

    it('blocks remote images by default and reports them', () => {
        const { html, hasBlockedImages } = sanitizeEmailHtml(
            '<img src="https://track.example/pixel.gif" alt="活动图"><img src="https://b.example/x.jpg">',
            false,
        );
        expect(hasBlockedImages).toBe(true);
        expect(html).not.toContain('track.example');
        expect(html).toContain('data-blocked-image');
        expect(html).toContain('活动图');
    });

    it('keeps remote images when the user allows them', () => {
        const { html, hasBlockedImages } = sanitizeEmailHtml(
            '<img src="https://pic.example/x.png" alt="a"><img srcset="https://pic.example/x2.png 2x">',
            true,
        );
        expect(hasBlockedImages).toBe(false);
        expect(html).toContain('src="https://pic.example/x.png"');
        expect(html).not.toContain('srcset');
    });

    it('keeps inline data images and drops data payloads that are not images', () => {
        const { html } = sanitizeEmailHtml(
            '<img src="data:image/png;base64,AAAA"><img src="data:text/html;base64,PHNjcmlwdD4=">',
            false,
        );
        expect(html).toContain('data:image/png;base64,AAAA');
        expect(html).not.toContain('data:text/html');
    });

    it('cleans the style attribute', () => {
        const { html } = sanitizeEmailHtml(
            '<p style="color:red;position:fixed;top:0;background:url(javascript:alert(1))">x</p>',
            false,
        );
        expect(html).not.toContain('position:fixed');
        expect(html).not.toContain('javascript');
        expect(html).toContain('color:red');
    });

    it('scopes stylesheet selectors to the reading pane', () => {
        const { html } = sanitizeEmailHtml(
            '<style>p { color: red } @media (max-width: 600px) { .box { padding: 2px } } @keyframes spin { 0% { opacity: 0 } }</style><p>body</p>',
            false,
        );
        expect(html).toContain('.email-html-body p { color: red }');
        expect(html).toContain('.email-html-body .box { padding: 2px }');
        expect(html).toContain('@media (max-width: 600px)');
        expect(html).not.toContain('@keyframes');
        // The pane class must not leak into declarations or at-rule preludes.
        expect(html).not.toContain('.email-html-body color');
        expect(html).not.toContain('.email-html-body @media');
    });

    it('drops unbalanced CSS instead of applying it', () => {
        const { html } = sanitizeEmailHtml('<style>p { color: red</style>', false);
        expect(html).not.toContain('<style');
    });

    it('strips @import and @namespace from stylesheets', () => {
        const { html } = sanitizeEmailHtml(
            '<style>@import url("https://evil.example/x.css"); @namespace svg url(http://www.w3.org/2000/svg); p { color: red }</style><p>x</p>',
            false,
        );
        expect(html).not.toContain('@import');
        expect(html).not.toContain('@namespace');
        expect(html).not.toContain('evil.example');
        expect(html).toContain('.email-html-body p { color: red }');
    });

    it('removes position:fixed from stylesheets so mail cannot fake app UI', () => {
        const { html } = sanitizeEmailHtml(
            '<style>.phish { position: fixed; inset: 0; background: #fff }</style><div class="phish">钓鱼</div>',
            false,
        );
        expect(html).not.toContain('position: fixed');
        expect(html).not.toContain('position:fixed');
        expect(html).toContain('inset: 0');
    });

    it('blocks remote url() in stylesheets unless pictures are allowed', () => {
        const css = '<style>.b { background: url(https://track.example/p.gif) }</style>';
        const blocked = sanitizeEmailHtml(css, false);
        expect(blocked.html).not.toContain('track.example');
        const allowed = sanitizeEmailHtml(css, true);
        expect(allowed.html).toContain('url(https://track.example/p.gif)');
    });

    it('returns empty output for empty input', () => {
        expect(sanitizeEmailHtml('', false)).toEqual({ html: '', hasBlockedImages: false });
        expect(sanitizeEmailHtml('   ', false)).toEqual({ html: '', hasBlockedImages: false });
    });

    it('removes form controls entirely', () => {
        const { html } = sanitizeEmailHtml('<form action="https://x"><input type="text"><button>go</button></form>', false);
        expect(html).not.toContain('<form');
        expect(html).not.toContain('<input');
        expect(html).not.toContain('<button');
    });
});
