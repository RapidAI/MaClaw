import { Component, Fragment, type ErrorInfo, type ReactNode } from 'react';
import { SettingsPanelFallback } from './SettingsPanelFallback';

type Props = {
    lang: string;
    /** Changes when the active settings tab changes — clears sticky error state. */
    resetKey: string;
    children: ReactNode;
};

type State = {
    error: Error | null;
    /** Bumped on Retry so children remount (needed after render/chunk errors). */
    retryCount: number;
};

/**
 * Catches failed settings-panel lazy chunks / render errors so the settings
 * rail stays usable instead of blanking the whole shell.
 */
export class SettingsPanelErrorBoundary extends Component<Props, State> {
    state: State = { error: null, retryCount: 0 };

    static getDerivedStateFromError(error: Error): Partial<State> {
        return { error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error('[SettingsPanelErrorBoundary]', error, info.componentStack);
    }

    componentDidUpdate(prevProps: Props) {
        if (prevProps.resetKey !== this.props.resetKey && this.state.error) {
            this.setState({ error: null, retryCount: 0 });
        }
    }

    private retry = () => {
        this.setState((prev) => ({ error: null, retryCount: prev.retryCount + 1 }));
    };

    render() {
        if (this.state.error) {
            const { lang } = this.props;
            const message = lang === 'zh-Hans'
                ? '\u8bbe\u7f6e\u52a0\u8f7d\u5931\u8d25\uff0c\u8bf7\u91cd\u8bd5\u3002'
                : lang === 'zh-Hant'
                    ? '\u8a2d\u5b9a\u8f09\u5165\u5931\u6557\uff0c\u8acb\u91cd\u8a66\u3002'
                    : 'Failed to load this settings panel. Please retry.';
            const actionLabel = lang === 'zh-Hans' || lang === 'zh-Hant' ? '\u91cd\u8bd5' : 'Retry';
            return <SettingsPanelFallback message={message} actionLabel={actionLabel} onAction={this.retry} />;
        }

        // Key forces a clean remount after Retry / tab switch recovery.
        return (
            <Fragment key={`${this.props.resetKey}:${this.state.retryCount}`}>
                {this.props.children}
            </Fragment>
        );
    }
}
