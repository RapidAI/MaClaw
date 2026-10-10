// @vitest-environment jsdom
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { BotMessageBody, botMessageIsStructured } from './botMessageBody';

describe('BotMessageBody', () => {
    it('leaves a plain command as text', () => {
        expect(botMessageIsStructured('你好')).toBe(false);
        const view = render(<BotMessageBody text="你好" />);
        expect(view.container.textContent).toBe('你好');
        expect(view.container.querySelector('strong, code, ul, ol')).toBeNull();
    });

    it('formats headings, inline code, and nested lists without raw markers', () => {
        const text = [
            '**错误原因分析**',
            '',
            '两次调用都失败了：',
            '- `app_list` 返回退出码 1',
            '',
            '1. **screenshot（截屏直录）**',
            '   不依赖 probe。',
            '   - 核验：看画面。',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelector('.desktop-bot-chat__heading')?.textContent).toBe('错误原因分析');
        expect(view.container.querySelector('code')?.textContent).toBe('app_list');
        expect(view.container.querySelector('ul')).toBeTruthy();
        const ordered = view.container.querySelector('ol');
        expect(ordered?.textContent).toContain('screenshot（截屏直录）');
        expect(ordered?.textContent).toContain('不依赖 probe。');
        expect(ordered?.querySelector('ul')?.textContent).toContain('核验：看画面。');
        expect(view.container.textContent).not.toContain('**');
        expect(view.container.textContent).not.toContain('`');
    });

    it('keeps later numbered items in the same list across a blank line', () => {
        const text = [
            '1. **screenshot（截屏直录）**',
            '   不依赖。',
            '',
            '   - 核验：看画面。',
            '',
            '2. **app_list 重试**',
            '   再试一次。',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        const lists = view.container.querySelectorAll('ol');
        expect(lists.length).toBe(1);
        expect(lists[0].querySelectorAll(':scope > li').length).toBe(2);
        expect(lists[0].textContent).toContain('不依赖。');
        expect(lists[0].querySelector('ul')?.textContent).toContain('核验：看画面。');
        expect(lists[0].textContent).toContain('再试一次。');
    });

    it('keeps an ordered list that starts after 1', () => {
        const view = render(<BotMessageBody text={'2. 第二项'} />);
        expect(view.container.querySelector('ol')?.getAttribute('start')).toBe('2');
    });

    it('renders a fenced block without the fence markers', () => {
        const view = render(<BotMessageBody text={'```\napp_list\n```'} />);
        const pre = view.container.querySelector('pre');
        expect(pre?.textContent).toBe('app_list');
        expect(view.container.textContent).not.toContain('```');
    });

    it('keeps the reply after an unclosed fence', () => {
        const view = render(<BotMessageBody text={'```\n未闭合\n\n**仍然是标题**'} />);
        expect(view.container.querySelector('pre')).toBeNull();
        expect(view.container.querySelector('strong')?.textContent).toBe('仍然是标题');
    });

    it('keeps the source number on each ordered item', () => {
        const view = render(<BotMessageBody text={'1. 第一\n\n3. 第三'} />);
        const items = view.container.querySelectorAll('ol > li');
        expect(items.length).toBe(2);
        expect(items[0].getAttribute('value')).toBe('1');
        expect(items[1].getAttribute('value')).toBe('3');
    });

    it('keeps a fenced block inside one list item, including its blank line and indent', () => {
        const text = [
            '1. 步骤',
            '   ```',
            '   if (x) {',
            '',
            '       return 1;',
            '   }',
            '   ```',
            '',
            '2. 下一步',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        const lists = view.container.querySelectorAll('ol');
        expect(lists.length).toBe(1);
        expect(lists[0].querySelectorAll(':scope > li').length).toBe(2);
        expect(lists[0].querySelector('pre')?.textContent).toBe('if (x) {\n\n    return 1;\n}');
        expect(lists[0].textContent).toContain('下一步');
        expect(view.container.textContent).not.toContain('```');
    });

    it('renders headings, quotes, rules, and tables without the raw markers', () => {
        const text = [
            '## 诊断结论',
            '',
            '经过多次尝试（ `app_list` 、 `screenshot` 等操作），所有桌面相关操作均返回相同错误：',
            '',
            '> **"this bot\'s desktop is the person\'s cloud desktop, and the connection is not configured"**',
            '',
            '**原因：** 当前机器人实例的 **云桌面连接未配置**。',
            '',
            '---',
            '',
            '## 任务结果',
            '',
            '| 操作 | 状态 |',
            '|------|------|',
            '| 打开浏览器 | ❌ 失败 |',
            '| 访问 `https://www.baidu.com` | ❌ 失败 |',
            '',
            '---',
            '',
            '### 建议',
            '',
            '请检查 MaClaw 云桌面服务配置。',
        ].join('\n');
        expect(botMessageIsStructured(text)).toBe(true);
        const view = render(<BotMessageBody text={text} />);
        const headings = view.container.querySelectorAll('h2, h3');
        expect(Array.from(headings).map(node => node.textContent)).toEqual(['诊断结论', '任务结果', '建议']);
        expect(view.container.querySelector('h2')?.className).toContain('desktop-bot-chat__heading');
        const quote = view.container.querySelector('blockquote');
        expect(quote?.textContent).toContain("person's cloud desktop");
        expect(quote?.textContent).not.toContain('>');
        expect(quote?.querySelector('strong')).toBeTruthy();
        expect(view.container.querySelectorAll('hr').length).toBe(2);
        const table = view.container.querySelector('table');
        expect(table?.querySelectorAll('th').length).toBe(2);
        expect(table?.querySelectorAll('tbody tr').length).toBe(2);
        expect(table?.textContent).toContain('打开浏览器');
        expect(table?.querySelector('code')?.textContent).toBe('https://www.baidu.com');
        expect(view.container.textContent).not.toContain('##');
        expect(view.container.textContent).not.toContain('|------|');
        expect(view.container.textContent).not.toContain('---');
    });

    it('keeps a pipe inside inline code in one table cell', () => {
        const text = [
            '| 命令 | 结果 |',
            '| --- | --- |',
            '| `echo a | wc` | 失败 |',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        const cells = view.container.querySelectorAll('td');
        expect(cells.length).toBe(2);
        expect(cells[0].querySelector('code')?.textContent).toBe('echo a | wc');
        expect(cells[1].textContent).toBe('失败');
    });

    it('renders a list-prefixed pipe table as a table', () => {
        const text = [
            '- | 操作 | 状态 |',
            '- | --- | --- |',
            '- | 打开浏览器 | 失败 |',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelector('ul')).toBeNull();
        expect(view.container.querySelectorAll('th').length).toBe(2);
        expect(view.container.querySelector('tbody')?.textContent).toContain('打开浏览器');
        expect(view.container.textContent).not.toContain('---');
    });

    it('leaves a bullet whose words contain a pipe as a list', () => {
        const view = render(<BotMessageBody text={'- 看 a | b 的差别\n- 下一项'} />);
        const list = view.container.querySelector('ul');
        expect(list?.querySelectorAll(':scope > li').length).toBe(2);
        expect(list?.textContent).toContain('a | b');
        expect(view.container.querySelector('table')).toBeNull();
    });

    it('renders code nested in bold without the markers', () => {
        const view = render(<BotMessageBody text={'调用 **`app_list`** 失败'} />);
        expect(view.container.querySelector('strong code')?.textContent).toBe('app_list');
        expect(view.container.textContent).toContain('调用');
        expect(view.container.textContent).toContain('失败');
        expect(view.container.textContent).not.toContain('**');
        expect(view.container.textContent).not.toContain('`');
    });

    it('renders a table whose delimiter and body omit the leading pipe', () => {
        const text = ['| 日期 | 天气 |', '--- | ---', '今天 | 晴'].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelectorAll('th').length).toBe(2);
        expect(view.container.querySelector('tbody')?.textContent).toContain('今天');
        expect(view.container.querySelector('tbody')?.textContent).toContain('晴');
        expect(view.container.querySelector('hr')).toBeNull();
        expect(view.container.textContent).not.toContain('---');
    });

    it('stops a table at a blank line', () => {
        const text = ['| 日期 | 天气 |', '今天 | 晴', '', '请在 a | b 之间选择'].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelector('tbody')?.textContent).toBe('今天晴');
        expect(view.container.querySelector('table')?.textContent).not.toContain('之间');
        expect(view.container.textContent).toContain('请在 a | b 之间选择');
    });

    it('renders consecutive pipe rows that have no separator line', () => {
        const view = render(<BotMessageBody text={'| 日期 | 天气 |\n| 今天 | 晴 |'} />);
        expect(view.container.querySelectorAll('th').length).toBe(2);
        expect(view.container.querySelector('tbody')?.textContent).toBe('今天晴');
        expect(view.container.querySelector('hr')).toBeNull();
    });

    it('renders a pipe table whose separator has a different column count', () => {
        const text = [
            '| 操作 | 状态 | 备注 |',
            '| --- | --- |',
            '| 打开 | 失败 | 超时 |',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelectorAll('th').length).toBe(3);
        expect(view.container.querySelector('tbody')?.textContent).toContain('超时');
    });

    it('treats a heading, quote, rule, or table as structured on its own', () => {
        expect(botMessageIsStructured('## 标题')).toBe(true);
        expect(botMessageIsStructured('> 引用')).toBe(true);
        expect(botMessageIsStructured('---')).toBe(true);
        expect(botMessageIsStructured('| 操作 | 状态 |\n|------|------|\n| 打开 | 失败 |')).toBe(true);
        expect(botMessageIsStructured('你好')).toBe(false);
    });

    it('renders a summary table whose body row has an extra pipe', () => {
        const text = [
            '**执行汇总：**',
            '',
            '| 步骤 | 结果 |',
            '|------|------|',
            '| 安装 | `g++` + `build-essential` + `libncurses-dev` | ✅ 成功 (g++ 12.2.0) |',
            '| 编译 | ✅ `g++ -o snake snake.cpp -lncurses` ，无错误 |',
            '| 运行 | ✅ 游戏已在终端窗口中运行，蛇已移动、吃到食物（截图可见 Score: 10） |',
            '| 截图 | ✅ `snake_screenshot.png` 已保存桌面并附于本条消息 |',
        ].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelectorAll('th').length).toBe(3);
        expect(view.container.querySelectorAll('tbody tr').length).toBe(4);
        expect(view.container.querySelector('code')?.textContent).toBe('g++');
        expect(view.container.textContent).toContain('build-essential');
        expect(view.container.textContent).toContain('snake_screenshot.png');
        expect(view.container.textContent).not.toContain('|');
        expect(view.container.textContent).not.toContain('------');
    });

    it('renders a table drawn with fullwidth pipes and dashes', () => {
        const text = ['｜ 步骤 ｜ 结果 ｜', '｜——————｜——————｜', '｜ 编译 ｜ 成功 ｜'].join('\n');
        const view = render(<BotMessageBody text={text} />);
        expect(view.container.querySelectorAll('th').length).toBe(2);
        expect(view.container.querySelector('tbody')?.textContent).toContain('编译');
        expect(view.container.querySelector('tbody')?.textContent).toContain('成功');
        expect(view.container.textContent).not.toContain('｜');
        expect(view.container.textContent).not.toContain('|');
        expect(view.container.textContent).not.toContain('—');
    });

    it('leaves the paragraph after a list outside the list', () => {
        const view = render(<BotMessageBody text={'1. 一项\n\n结论在列表外。'} />);
        const list = view.container.querySelector('ol');
        expect(list?.textContent).toBe('一项');
        expect(list?.textContent).not.toContain('结论');
        expect(view.container.textContent).toContain('结论在列表外。');
    });
});
