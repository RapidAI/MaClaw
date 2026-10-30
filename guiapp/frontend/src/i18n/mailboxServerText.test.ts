import { describe, expect, it } from 'vitest';
import { localizeText } from './index';
import { localizeMailboxServerText } from './mailboxServerText';

describe('mailbox Mongolian copy', () => {
    it('fills delete and rename sentences from the English the screen builds', () => {
        expect(localizeText('mn', 'Permanently delete “Quarterly”? This cannot be undone.', '彻底', '徹底'))
            .toBe('«Quarterly»-г бүрмөсөн устгах уу? Буцаах боломжгүй.');
        expect(localizeText('mn', 'Move “Quarterly” to the trash folder?', '移', '移'))
            .toBe('«Quarterly»-г устгасан хавтас руу зөөх үү?');
        expect(localizeText('mn', 'Permanently delete 3 selected message(s)? This cannot be undone.', '彻底', '徹底'))
            .toBe('Сонгосон 3 захидлыг бүрмөсөн устгах уу? Буцаах боломжгүй.');
        expect(localizeText('mn', 'Move 3 selected message(s) to the trash folder?', '移', '移'))
            .toBe('Сонгосон 3 захидлыг устгасан хавтас руу зөөх үү?');
        expect(localizeText('mn', 'Delete mailbox “Work”? This cannot be undone.', '删', '刪'))
            .toBe('«Work» имэйлийг устгах уу? Буцаах боломжгүй.');
        expect(localizeText('mn', 'Delete signature “Personal”? This cannot be undone.', '删', '刪'))
            .toBe('«Personal» гарын үсгийг устгах уу? Буцаах боломжгүй.');
        expect(localizeText('mn', 'Editing “Work”', '编', '編')).toBe('«Work»-г засаж байна');
        expect(localizeText('mn', '{{count}} unread', '未读', '未讀')).toBe('{{count}} уншаагүй');
        expect(localizeText('mn', '{{count}} selected', '已选', '已選')).toBe('{{count}} сонгогдсон');
        expect(localizeText('zh-Hans', 'Manage mail', '邮箱管理', '郵箱管理')).toBe('邮箱管理');
        expect(localizeText('en', 'Manage mail', '邮箱管理', '郵箱管理')).toBe('Manage mail');
        expect(localizeText('mn', 'Archive', '归档', '封存')).toBe('Архивлах');
        expect(localizeText('mn', 'Mail archive', '归档', '封存')).toBe('Архив');
    });

    it('translates fixed mailbox server sentences only for Mongolian', () => {
        expect(localizeMailboxServerText('mn', '收件连接正常')).toBe('Хүлээн авах холболт хэвийн');
        expect(localizeMailboxServerText('mn', '发件连接正常')).toBe('Илгээх холболт хэвийн');
        expect(localizeMailboxServerText('mn', '邮箱「Work」没有配置收件')).toBe('«Work» имэйлд хүлээн авалт тохируулаагүй');
        expect(localizeMailboxServerText('mn', '一次最多删除 100 封邮件')).toBe('Нэг удаад хамгийн ихдээ 100 захидал устгана');
        expect(localizeMailboxServerText('mn', 'Gmail 邮箱 a@gmail.com 已登录')).toBe('Gmail имэйл a@gmail.com нэвтэрсэн');
        expect(localizeMailboxServerText('mn', 'Hotmail 邮箱 b@outlook.com 已登录')).toBe('Hotmail имэйл b@outlook.com нэвтэрсэн');
        expect(localizeMailboxServerText('mn', '登录已取消')).toBe('Нэвтрэлтийг цуцалсан');
        expect(localizeMailboxServerText('mn', '登录超时，请重试')).toBe('Нэвтрэлтийн хугацаа дууссан. Дахин оролдоно уу.');
        expect(localizeMailboxServerText('mn', '登录失败，授权没有完成')).toBe('Нэвтрэлт амжилтгүй. Зөвшөөрөл дуусаагүй.');
        expect(localizeMailboxServerText('zh-Hans', '收件连接正常')).toBe('收件连接正常');
        expect(localizeMailboxServerText('zh-Hans', 'Gmail 邮箱 a@gmail.com 已登录')).toBe('Gmail 邮箱 a@gmail.com 已登录');
        expect(localizeMailboxServerText('en', '收件连接正常')).toBe('收件连接正常');
        expect(localizeMailboxServerText('en', '登录已取消')).toBe('登录已取消');
        expect(localizeMailboxServerText('mn', '一些别的错误')).toBe('一些别的错误');
    });
});
