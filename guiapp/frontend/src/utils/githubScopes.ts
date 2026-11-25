import { localizeText } from '../i18n/langSelect';

// The exact OAuth scope "repo" — public_repo / repo:status do not grant
// repository checkout or push on cloud coding instances.
export function scopesHaveRepo(scopes?: string): boolean {
    return String(scopes || '').split(/[\s,]+/).some((part) => part === 'repo');
}

// The signed-in account can lack repository permission for three different
// reasons, and each needs its own guidance: a ghu_ GitHub App token carries
// no OAuth scopes at all (only a fresh OAuth App login fixes it), an OAuth
// App token without the repo scope usually means the Repositories checkbox
// was unchecked on the authorization page, and GitLab PATs are re-minted
// with api/write_repository.
export function noRepoScopeNote(lang: string, isGitLab: boolean, githubAppToken?: boolean): string {
    const text = (en: string, hans: string, hant?: string) => localizeText(lang, en, hans, hant);
    if (isGitLab) {
        return text(
            'The GitLab token has no repository permission (it needs the api or write_repository scope). Mint a new PAT with that scope and paste it in Settings → GitHub & GitLab.',
            '当前 GitLab 令牌没有仓库权限（需要 api 或 write_repository 权限）。请在浏览器创建带该权限的新令牌，在 设置 → GitHub & GitLab 重新粘贴。',
            '目前 GitLab 權杖沒有倉庫權限（需要 api 或 write_repository 權限）。請在瀏覽器建立帶該權限的新權杖，在 設定 → GitHub & GitLab 重新貼上。');
    }
    return githubAppToken
        ? text(
            'The signed-in token is a GitHub App token with no repository permission (usually left over from an older login). Disconnect GitHub in Settings → GitHub & GitLab, then sign in again — or paste a token with repo access under Advanced.',
            '当前登录的是 GitHub App 令牌，不带仓库权限（通常是旧版登录遗留）。请在 设置 → GitHub & GitLab 断开 GitHub 后重新登录，或在高级选项里粘贴带 repo 权限的令牌。',
            '目前登入的是 GitHub App 權杖，不帶倉庫權限（通常是舊版登入遺留）。請在 設定 → GitHub & GitLab 中斷 GitHub 後重新登入，或在進階選項裡貼上帶 repo 權限的權杖。')
        : text(
            'The signed-in token has no repository permission. Reconnect GitHub in Settings → GitHub & GitLab (check Repositories on the authorization page), or paste a token with repo access under Advanced.',
            '当前登录的 GitHub 令牌没有仓库权限。请在 设置 → GitHub & GitLab 断开后重新登录（授权页勾选 Repositories 权限），或在高级选项里粘贴带 repo 权限的令牌。',
            '目前登入的 GitHub 權杖沒有倉庫權限。請在 設定 → GitHub & GitLab 中斷後重新登入（授權頁勾選 Repositories 權限），或在進階選項裡貼上帶 repo 權限的權杖。');
}
