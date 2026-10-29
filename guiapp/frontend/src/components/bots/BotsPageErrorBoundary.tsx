import { Component, Fragment, type ErrorInfo, type ReactNode } from 'react';
import { localizeText } from '../../i18n';
import { reportFrontendIssue } from '../../frontendIssueReport';

type Props = { lang: string; children: ReactNode };
type State = { error: Error | null; retry: number };

// Keeps a Bot page render failure inside the page: the rest of the app
// stays usable, the failure reaches maclaw.log, and Retry remounts it.
export class BotsPageErrorBoundary extends Component<Props, State> {
    state: State = { error: null, retry: 0 };

    static getDerivedStateFromError(error: Error): Partial<State> {
        return { error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error('[BotsPageErrorBoundary]', error, info.componentStack);
        reportFrontendIssue('bots-page-crash', `${error?.name || 'Error'}: ${error?.message || ''}`, {
            stack: String(info?.componentStack || '').split('\n').map(line => line.trim()).filter(Boolean).slice(0, 5).join(' < ').slice(0, 240),
        });
    }

    render() {
        if (!this.state.error) return <Fragment key={this.state.retry}>{this.props.children}</Fragment>;
        const lang = this.props.lang;
        return (
            <div className="desktop-bot-workspace__crash" data-testid="bots-page-crash" role="alert" style={{ padding: 24 }}>
                <p>{localizeText(lang, 'The Bot page hit an error and was stopped.', 'Bot 页面出错了，已停下。')}</p>
                <button type="button" onClick={() => this.setState(prev => ({ error: null, retry: prev.retry + 1 }))}>{localizeText(lang, 'Try again', '重试')}</button>
            </div>
        );
    }
}
