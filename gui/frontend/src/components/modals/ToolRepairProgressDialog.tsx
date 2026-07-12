import { StatusGlyph } from '../ai/WorkbenchIcons';

type ToolRepairStatus = {
    show: boolean;
    toolName: string;
    status: 'installing' | 'success' | 'failed';
    message: string;
};

type ToolRepairProgressDialogProps = {
    status: ToolRepairStatus;
    t: (key: string) => string;
    onClose: () => void;
};

export const ToolRepairProgressDialog = ({ status, t, onClose }: ToolRepairProgressDialogProps) => (
    <div className="modal-overlay tool-repair-overlay">
        <div className="tool-repair-dialog">
            {status.status === 'installing' && (
                <div className="tool-repair-dialog__row tool-repair-dialog__row--installing">
                    <div className="tool-repair-dialog__spinner" />
                    <span className="tool-repair-dialog__text">
                        {t("toolRepairInstalling").replace("{tool}", status.toolName)}
                    </span>
                </div>
            )}
            {status.status === 'success' && (
                <div className="tool-repair-dialog__row tool-repair-dialog__row--success">
                    <div className="tool-repair-dialog__status-icon" data-status="success" aria-hidden="true">
                        <StatusGlyph kind="ok" size={16} />
                    </div>
                    <span className="tool-repair-dialog__text" data-status="success">
                        {t("toolRepairSuccess").replace("{tool}", status.toolName)}
                    </span>
                </div>
            )}
            {status.status === 'failed' && (
                <div>
                    <div className="tool-repair-dialog__row tool-repair-dialog__row--failed">
                        <div className="tool-repair-dialog__status-icon" data-status="failed" aria-hidden="true">
                            <StatusGlyph kind="error" size={16} />
                        </div>
                        <span className="tool-repair-dialog__text" data-status="failed">
                            {t("toolRepairFailed").replace("{tool}", status.toolName)}
                        </span>
                    </div>
                    <p className="tool-repair-dialog__message">
                        {status.message}
                    </p>
                    <button className="tool-repair-dialog__close" onClick={onClose}>
                        {t("close")}
                    </button>
                </div>
            )}
        </div>
    </div>
);
