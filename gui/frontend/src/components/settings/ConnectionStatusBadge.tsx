import { StatusGlyph } from '../ai/WorkbenchIcons';
import { connectionStatusGlyphKind, connectionStatusLabel } from './imSettingsShared';

/** IM channel connection status with SVG glyph (never emoji). */
export function ConnectionStatusBadge({ status, lang }: { status: string; lang: string }) {
    const label = connectionStatusLabel(status, lang);
    return (
        <span
            className="im-settings-status"
            data-status={status}
            style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}
            title={label}
        >
            <span aria-hidden="true" style={{ display: 'inline-flex' }}>
                <StatusGlyph kind={connectionStatusGlyphKind(status)} size={12} />
            </span>
            {label}
        </span>
    );
}
