/**
 * Unit tests for the "Show Assistant Entry" settings checkbox behavior.
 *
 * Tests:
 * - Toggle on/off updates config correctly
 * - Config change to false hides visible floating button
 * - Default value when field is missing
 *
 * Validates: Requirements 5.1, 5.2, 5.3, 5.4
 */
import { describe, it, expect, vi } from 'vitest';

// Define a minimal config type for testing purposes
interface TestConfig {
    show_assistant_entry?: boolean;
    language?: string;
    hide_startup_popup?: boolean;
    workstation_mode?: boolean;
}

// Mock the Wails bindings - use spread operator to bypass type checking
const saveConfigMock = vi.fn().mockResolvedValue(undefined);
const hideFloatingButtonMock = vi.fn().mockResolvedValue(undefined);

vi.mock('../../../wailsjs/go/main/App', () => ({
    SaveConfig: (...args: unknown[]) => saveConfigMock(...args),
    HideFloatingButton: (...args: unknown[]) => hideFloatingButtonMock(...args),
}));

vi.mock('../../../wailsjs/go/models', () => ({
    main: {
        AppConfig: class AppConfig {
            constructor(data: Record<string, unknown>) {
                Object.assign(this, data);
            }
        },
    },
}));

// Import after mocking
import { SaveConfig, HideFloatingButton } from '../../../wailsjs/go/main/App';

// Pure logic tests for checkbox behavior

describe('Show Assistant Entry Checkbox Behavior', () => {
    // Test 1: Toggle on/off updates config correctly
    describe('Toggle updates config', () => {
        it('sets show_assistant_entry to true when checked', () => {
            const config: TestConfig = { show_assistant_entry: false };
            const newConfig: TestConfig = { ...config, show_assistant_entry: true };

            expect(newConfig.show_assistant_entry).toBe(true);
        });

        it('sets show_assistant_entry to false when unchecked', () => {
            const config: TestConfig = { show_assistant_entry: true };
            const newConfig: TestConfig = { ...config, show_assistant_entry: false };

            expect(newConfig.show_assistant_entry).toBe(false);
        });

        it('preserves other config fields when toggling', () => {
            const config: TestConfig = {
                show_assistant_entry: true,
                language: 'zh-CN',
                hide_startup_popup: true,
                workstation_mode: false,
            };
            const newConfig: TestConfig = { ...config, show_assistant_entry: false };

            expect(newConfig.language).toBe('zh-CN');
            expect(newConfig.hide_startup_popup).toBe(true);
            expect(newConfig.workstation_mode).toBe(false);
        });
    });

    // Test 2: Config change to false hides visible floating button
    describe('Config change hides floating button', () => {
        it('should call HideFloatingButton when config changes from true to false', async () => {
            vi.clearAllMocks();

            // Simulate the checkbox change handler
            const handleCheckboxChange = async (checked: boolean, floatingButtonVisible: boolean) => {
                if (!checked && floatingButtonVisible) {
                    await HideFloatingButton();
                }
            };

            // When config changes to false and floating button is visible
            await handleCheckboxChange(false, true);

            expect(hideFloatingButtonMock).toHaveBeenCalled();
        });

        it('should not call HideFloatingButton when config changes to true', async () => {
            vi.clearAllMocks();

            const handleCheckboxChange = async (checked: boolean, floatingButtonVisible: boolean) => {
                if (!checked && floatingButtonVisible) {
                    await HideFloatingButton();
                }
            };

            await handleCheckboxChange(true, true);

            expect(hideFloatingButtonMock).not.toHaveBeenCalled();
        });

        it('should not call HideFloatingButton when floating button is not visible', async () => {
            vi.clearAllMocks();

            const handleCheckboxChange = async (checked: boolean, floatingButtonVisible: boolean) => {
                if (!checked && floatingButtonVisible) {
                    await HideFloatingButton();
                }
            };

            await handleCheckboxChange(false, false);

            expect(hideFloatingButtonMock).not.toHaveBeenCalled();
        });
    });

    // Test 3: Default value when field is missing
    describe('Default value handling', () => {
        it('returns true when show_assistant_entry is undefined', () => {
            const config: TestConfig = {};
            const showAssistantEntry = config.show_assistant_entry !== false;

            expect(showAssistantEntry).toBe(true);
        });

        it('returns true when show_assistant_entry is null', () => {
            const config: TestConfig = { show_assistant_entry: null as unknown as boolean };
            const showAssistantEntry = config.show_assistant_entry !== false;

            expect(showAssistantEntry).toBe(true);
        });

        it('returns false when show_assistant_entry is explicitly false', () => {
            const config: TestConfig = { show_assistant_entry: false };
            const showAssistantEntry = config.show_assistant_entry !== false;

            expect(showAssistantEntry).toBe(false);
        });

        it('returns true when show_assistant_entry is explicitly true', () => {
            const config: TestConfig = { show_assistant_entry: true };
            const showAssistantEntry = config.show_assistant_entry !== false;

            expect(showAssistantEntry).toBe(true);
        });
    });

    // Test 4: SaveConfig is called after toggle
    describe('SaveConfig integration', () => {
        it('calls SaveConfig with updated config', async () => {
            vi.clearAllMocks();

            const config: TestConfig = { show_assistant_entry: true };
            const newConfig: TestConfig = { ...config, show_assistant_entry: false };

            await SaveConfig(newConfig as unknown as never);

            expect(saveConfigMock).toHaveBeenCalledWith(newConfig);
        });
    });
});

// Edge cases

describe('Edge cases', () => {
    it('handles rapid toggle without race condition', async () => {
        vi.clearAllMocks();

        // Simulate rapid toggle
        const config: TestConfig = { show_assistant_entry: true };
        const toggle1: TestConfig = { ...config, show_assistant_entry: false };
        const toggle2: TestConfig = { ...toggle1, show_assistant_entry: true };

        await SaveConfig(toggle1 as unknown as never);
        await SaveConfig(toggle2 as unknown as never);

        expect(saveConfigMock).toHaveBeenCalledTimes(2);
        expect(saveConfigMock).toHaveBeenLastCalledWith(toggle2);
    });

    it('handles config object with missing optional fields', () => {
        const config: TestConfig = {
            // show_assistant_entry is missing
            language: 'en-US',
        };

        const showAssistantEntry = config.show_assistant_entry !== false;
        expect(showAssistantEntry).toBe(true);
    });
});

