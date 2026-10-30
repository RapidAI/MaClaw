import { describe, expect, it } from 'vitest';
import { createLatestTicket, messengerCallbackURL, messengerPublicBaseIsInsecure } from '../MessengerSettings';

describe('messengerCallbackURL', () => {
    it('uses the local listener until a public base URL is set', () => {
        expect(messengerCallbackURL(null)).toEqual({
            callback: 'http://127.0.0.1:8791/facebook-messenger/webhook',
            local: 'http://127.0.0.1:8791/facebook-messenger/webhook',
        });
    });

    it('pastes the public HTTPS origin plus the webhook path', () => {
        expect(messengerCallbackURL({
            messenger_public_base_url: 'https://example.com/',
            messenger_webhook_path: '/facebook-messenger/webhook',
            messenger_listen_host: '0.0.0.0',
            messenger_listen_port: 9000,
        })).toEqual({
            callback: 'https://example.com/facebook-messenger/webhook',
            local: 'http://127.0.0.1:9000/facebook-messenger/webhook',
        });
    });

    it('flags an http public base because Meta requires HTTPS', () => {
        expect(messengerPublicBaseIsInsecure('http://example.com')).toBe(true);
        expect(messengerPublicBaseIsInsecure('https://example.com')).toBe(false);
    });

    it('hides userinfo and does not append the webhook path twice', () => {
        expect(messengerCallbackURL({
            messenger_public_base_url: 'https://user:pass@example.com/facebook-messenger/webhook?x=1',
        }).callback).toBe('https://example.com/facebook-messenger/webhook');
        expect(messengerCallbackURL({
            messenger_public_base_url: 'https://user:pass@example.com/hook/',
            messenger_webhook_path: '/facebook-messenger/webhook',
        }).callback).toBe('https://example.com/hook/facebook-messenger/webhook');
    });

    it('uses the default path when the draft is only slashes', () => {
        expect(messengerCallbackURL({
            messenger_public_base_url: 'https://example.com',
            messenger_webhook_path: '//',
        }).callback).toBe('https://example.com/facebook-messenger/webhook');
    });

    it('drops an older status read once a newer read has started', () => {
        const tickets = createLatestTicket();
        const first = tickets.issue();
        const second = tickets.issue();
        expect(tickets.matches(first)).toBe(false);
        expect(tickets.matches(second)).toBe(true);
    });

    it('brackets an IPv6 listen host in the local URL', () => {
        expect(messengerCallbackURL({
            messenger_listen_host: '::1',
            messenger_listen_port: 8791,
        }).local).toBe('http://[::1]:8791/facebook-messenger/webhook');
    });
});
