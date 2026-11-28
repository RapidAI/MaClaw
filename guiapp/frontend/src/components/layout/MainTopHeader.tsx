import { type Dispatch, type MouseEvent as ReactMouseEvent, type SetStateAction } from 'react';
import { getAllToolOptions, isToolTab, normalizeToolTab } from '../../config/toolCatalog';
import { windowDragHandleProps } from '../../utils/windowDrag';
import { getHeaderTitle } from './mainTopHeaderTitle';
import { MainTopHeaderActions } from './MainTopHeaderActions';
import { SidebarToggleCluster } from './SidebarToggleCluster';
import { WindowCloseIcon, WindowMaximizeIcon, WindowRestoreIcon } from './WindowControlIcons';
import { BugReportButton } from '../ai/BugReportButton';
import { openDesktopBotRoomsFromHeader, useDesktopBotRoomsVisible } from '../bots/desktopBotHeaderBridge';
import { localizeText } from '../../i18n';


interface MainTopHeaderProps {
    navTab: string;
    /** Current settings tab; lets the header name the Knowledge page opened from the library menu. */
    settingsTab?: string;
    lang: string;
    t: (key: string) => string;
    activeTool: string;
    switchTool: (tool: string) => void;
    handleAddNewProject: () => void;
    setRefreshStatus: Dispatch<SetStateAction<string>>;
    setTutorialContent: Dispatch<SetStateAction<string>>;
    setRefreshKey: Dispatch<SetStateAction<number>>;
    setShowModelSettings: Dispatch<SetStateAction<boolean>>;
    setSelectedSkillsToInstall: Dispatch<SetStateAction<string[]>>;
    setShowInstallSkillModal: Dispatch<SetStateAction<boolean>>;
    handleWindowHide: (e: ReactMouseEvent) => void;
    handleWindowMaximizeToggle: (e?: ReactMouseEvent) => void;
    windowMaximized: boolean;
    /** 功能栏 (nav rail) collapse state for the leading toggle; the task pane does not exist on these tabs. */
    railCollapsed?: boolean;
    onToggleRail?: () => void;
}

const zhHans = {
    hideWindow: '\u9690\u85cf\u7a97\u53e3',
    maximizeWindow: '\u6700\u5927\u5316\u7a97\u53e3',
    restoreWindow: '\u8fd8\u539f\u7a97\u53e3',
};

const windowControlBtnStyle = {
    '--wails-draggable': 'no-drag',
    pointerEvents: 'auto',
    cursor: 'pointer',
    position: 'relative',
    zIndex: 10001,
    width: '36px',
    height: '28px',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'transparent',
    border: 'none',
    borderRadius: '4px',
    padding: 0,
    lineHeight: 1,
    flexShrink: 0,
    color: 'var(--theme-text-secondary)',
    transition: 'background 120ms ease, color 120ms ease',
} as any;

export const MainTopHeader = ({
    navTab,
    settingsTab,
    lang,
    t,
    activeTool,
    switchTool,
    handleAddNewProject,
    setRefreshStatus,
    setTutorialContent,
    setRefreshKey,
    setShowModelSettings,
    setSelectedSkillsToInstall,
    setShowInstallSkillModal,
    handleWindowHide,
    handleWindowMaximizeToggle,
    windowMaximized,
    railCollapsed = false,
    onToggleRail,
}: MainTopHeaderProps) => {
    const safeActiveTool = normalizeToolTab(activeTool);
    const toolOptions = getAllToolOptions();
    const showToolSwitcher = isToolTab(navTab);
    // bots 页签把 群聊/故障报告 从桌面 Bot 悬浮标题条移到了这里：群聊按钮
    // 由 DesktopBotWorkspace 经 desktopBotHeaderBridge 登记（只在选中 Bot 且
    // hub 开了群聊时出现），故障报告是窗口级控件，bots 页签常驻。hook 必须
    // 无条件调用，可见性再与 navTab 合取。
    const botRoomsRegistered = useDesktopBotRoomsVisible();
    const roomsVisible = navTab === 'bots' && botRoomsRegistered;
    return (
    <div className="top-header" {...windowDragHandleProps(true, { userSelect: 'none' })} onDoubleClick={() => handleWindowMaximizeToggle()}>
        <div className="mth-row">
            <SidebarToggleCluster showPaneToggle={false} paneCollapsed={false} railCollapsed={railCollapsed} onToggleRail={onToggleRail} lang={lang} />
            <h2 className="mth-title">
                <span className="mc-header-brand" aria-label="MaClaw">
                    <span>MaClaw</span>
                </span>
                {navTab !== 'ai' && (showToolSwitcher ? (
                    <select
                        className="top-header-tool-select"
                        value={safeActiveTool}
                        aria-label={localizeText(lang, 'Coding tool', '编程工具')}
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => event.stopPropagation()}
                        onDoubleClick={(event) => event.stopPropagation()}
                        onChange={(event) => switchTool(event.target.value)}
                    >
                        {toolOptions.map((tool) => (
                            <option key={tool.id} value={tool.id}>{tool.name}</option>
                        ))}
                    </select>
                ) : (
                    <span>{getHeaderTitle(navTab, lang, t, true, settingsTab)}</span>
                ))}
                <MainTopHeaderActions
                    navTab={navTab}
                    lang={lang}
                    t={t}
                    activeTool={safeActiveTool}
                    switchTool={switchTool}
                    handleAddNewProject={handleAddNewProject}
                    setRefreshStatus={setRefreshStatus}
                    setTutorialContent={setTutorialContent}
                    setRefreshKey={setRefreshKey}
                    setShowModelSettings={setShowModelSettings}
                    setSelectedSkillsToInstall={setSelectedSkillsToInstall}
                    setShowInstallSkillModal={setShowInstallSkillModal}
                />
            </h2>
            {/* bots 页签的动作簇是 .top-header-window-controls 的兄弟节点而不是
                子节点：pet-store 的 `.top-header-window-controls button` 会把
                控制区按钮强制钳到 32x32（!important，为图标钮设计），文本按钮
                「群聊」放进那个容器必然破版。 */}
            {navTab === 'bots' ? (
                // data-window-no-drag + mousedown stop mirror
                // SidebarToggleCluster: the cluster sits in a window-drag
                // region, and a bubbling double-click would toggle
                // maximize on the header.
                <div
                    className="mth-bot-actions"
                    data-window-no-drag
                    onMouseDown={(event) => event.stopPropagation()}
                    onDoubleClick={(event) => event.stopPropagation()}
                >
                    {roomsVisible ? (
                        <button
                            type="button"
                            className="btn-secondary mth-bot-rooms-btn"
                            data-testid="bot-rooms-open"
                            onClick={openDesktopBotRoomsFromHeader}
                        >群聊</button>
                    ) : null}
                    <BugReportButton lang={lang} />
                </div>
            ) : null}
            <div className="top-header-window-controls mth-window-controls">
                <span className="mc-header-ready"><i aria-hidden="true" />{localizeText(lang, 'Ready', '准备就绪')}</span>
                <button
                    onMouseDown={handleWindowHide}
                    // Stop the double-click: the header's dblclick-to-maximize
                    // would otherwise fire on the just-hidden window and leave
                    // it maximized when the user restores it from the tray.
                    onDoubleClick={(event) => event.stopPropagation()}
                    aria-label={localizeText(lang, 'Hide window', zhHans.hideWindow)}
                    title={localizeText(lang, 'Hide window', zhHans.hideWindow)}
                    style={windowControlBtnStyle}
                >
                    <WindowCloseIcon />
                </button>
                <button
                    onMouseDown={handleWindowMaximizeToggle}
                    // Two mousedowns of a double-click already toggle and
                    // revert; the header's dblclick handler would add a third
                    // toggle and leave the window maximized.
                    onDoubleClick={(event) => event.stopPropagation()}
                    aria-label={windowMaximized ? localizeText(lang, 'Restore window', zhHans.restoreWindow) : localizeText(lang, 'Maximize window', zhHans.maximizeWindow)}
                    title={windowMaximized ? localizeText(lang, 'Restore window', zhHans.restoreWindow) : localizeText(lang, 'Maximize window', zhHans.maximizeWindow)}
                    style={windowControlBtnStyle}
                >
                    {windowMaximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}
                </button>
            </div>
        </div>
    </div>
    );
};
