import { localizeText } from '../../i18n';
import { DownloadCloudAgentWorkspace } from '../../../wailsjs/go/main/App';
import type { RemoteAssistantSession } from '../layout/remoteAssistantSession';

// The card-head 导出 packs the whole cloud workspace into one .tar.gz —
// the same srv call the task list's 打包下载 context entry runs. The
// session's projectPath carries the instance id as "cloud-agent:<id>".
export async function downloadRemoteWorkspace(chat: RemoteAssistantSession | null, lang: string): Promise<string> {
    if (!chat || !chat.cloud) return '';
    const instanceId = String(chat.projectPath || '').replace(/^cloud-agent:/, '').trim();
    if (!instanceId) throw new Error(localizeText(lang, 'Missing cloud task', '缺少云端任务'));
    return String(await DownloadCloudAgentWorkspace(instanceId, (chat.taskName || instanceId).trim()) || '');
}
