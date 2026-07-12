import { useState, useEffect } from 'react'
import {
    Plus,
    Trash2,
    CheckCircle2,
    Play,
    X,
    Server,
    ShieldCheck,
    Copy,
    Check,
    ChevronLeft,
    ChevronRight,
    ChevronDown,
    Loader2,
} from 'lucide-react'
import clsx from 'clsx'
import { useI18n } from '../i18n'

export default function AccountManager({ config, onRefresh, onMessage, authFetch }) {
    const { t } = useI18n()
    const [showAddKey, setShowAddKey] = useState(false)
    const [showAddAccount, setShowAddAccount] = useState(false)
    const [newKey, setNewKey] = useState('')
    const [copiedKey, setCopiedKey] = useState(null)
    const [newAccount, setNewAccount] = useState({ email: '', mobile: '', password: '' })
    const [loading, setLoading] = useState(false)
    const [testing, setTesting] = useState({})
    const [testingAll, setTestingAll] = useState(false)
    const [batchProgress, setBatchProgress] = useState({ current: 0, total: 0, results: [] })
    const [queueStatus, setQueueStatus] = useState(null)
    const [keysExpanded, setKeysExpanded] = useState(false)

    // 分页状态
    const [accounts, setAccounts] = useState([])
    const [page, setPage] = useState(1)
    const [pageSize] = useState(10)
    const [totalPages, setTotalPages] = useState(1)
    const [totalAccounts, setTotalAccounts] = useState(0)
    const [loadingAccounts, setLoadingAccounts] = useState(false)

    const apiFetch = authFetch || fetch
    const resolveAccountIdentifier = (acc) => {
        if (!acc || typeof acc !== 'object') return ''
        return String(acc.identifier || acc.email || acc.mobile || '').trim()
    }

    const fetchAccounts = async (targetPage = page) => {
        setLoadingAccounts(true)
        try {
            const res = await apiFetch(`/admin/accounts?page=${targetPage}&page_size=${pageSize}`)
            if (res.ok) {
                const data = await res.json()
                setAccounts(data.items || [])
                setTotalPages(data.total_pages || 1)
                setTotalAccounts(data.total || 0)
                setPage(data.page || 1)
            }
        } catch (e) {
            console.error('Failed to fetch accounts:', e)
        } finally {
            setLoadingAccounts(false)
        }
    }

    const fetchQueueStatus = async () => {
        try {
            const res = await apiFetch('/admin/queue/status')
            if (res.ok) {
                const data = await res.json()
                setQueueStatus(data)
            }
        } catch (e) {
            console.error('Failed to fetch queue status:', e)
        }
    }

    useEffect(() => {
        fetchAccounts()
        fetchQueueStatus()
        const interval = setInterval(fetchQueueStatus, 5000)
        return () => clearInterval(interval)
    }, [])

    const addKey = async () => {
        if (!newKey.trim()) return
        setLoading(true)
        try {
            const res = await apiFetch('/admin/keys', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ key: newKey.trim() }),
            })
            if (res.ok) {
                onMessage('success', t('accountManager.addKeySuccess'))
                setNewKey('')
                setShowAddKey(false)
                onRefresh()
            } else {
                const data = await res.json()
                onMessage('error', data.detail || t('messages.failedToAdd'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setLoading(false)
        }
    }

    const deleteKey = async (key) => {
        if (!confirm(t('accountManager.deleteKeyConfirm'))) return
        try {
            const res = await apiFetch(`/admin/keys/${encodeURIComponent(key)}`, { method: 'DELETE' })
            if (res.ok) {
                onMessage('success', t('messages.deleted'))
                onRefresh()
            } else {
                onMessage('error', t('messages.deleteFailed'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        }
    }

    const addAccount = async () => {
        if (!newAccount.password || (!newAccount.email && !newAccount.mobile)) {
            onMessage('error', t('accountManager.requiredFields'))
            return
        }
        setLoading(true)
        try {
            const res = await apiFetch('/admin/accounts', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newAccount),
            })
            if (res.ok) {
                onMessage('success', t('accountManager.addAccountSuccess'))
                setNewAccount({ email: '', mobile: '', password: '' })
                setShowAddAccount(false)
                fetchAccounts(1) // 添加后回到第一页
                onRefresh()
            } else {
                const data = await res.json()
                onMessage('error', data.detail || t('messages.failedToAdd'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        } finally {
            setLoading(false)
        }
    }

    const deleteAccount = async (id) => {
        const identifier = String(id || '').trim()
        if (!identifier) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        if (!confirm(t('accountManager.deleteAccountConfirm'))) return
        try {
            const res = await apiFetch(`/admin/accounts/${encodeURIComponent(identifier)}`, { method: 'DELETE' })
            if (res.ok) {
                onMessage('success', t('messages.deleted'))
                fetchAccounts() // 刷新当前页
                onRefresh()
            } else {
                onMessage('error', t('messages.deleteFailed'))
            }
        } catch (e) {
            onMessage('error', t('messages.networkError'))
        }
    }

    const testAccount = async (identifier) => {
        const accountID = String(identifier || '').trim()
        if (!accountID) {
            onMessage('error', t('accountManager.invalidIdentifier'))
            return
        }
        setTesting(prev => ({ ...prev, [accountID]: true }))
        try {
            const res = await apiFetch('/admin/accounts/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ identifier: accountID }),
            })
            const data = await res.json()
            const statusMessage = data.success
                ? t('apiTester.testSuccess', { account: accountID, time: data.response_time })
                : `${accountID}: ${data.message}`
            onMessage(data.success ? 'success' : 'error', statusMessage)
            fetchAccounts() // 刷新当前页
            onRefresh()
        } catch (e) {
            onMessage('error', t('accountManager.testFailed', { error: e.message }))
        } finally {
            setTesting(prev => ({ ...prev, [accountID]: false }))
        }
    }

    const testAllAccounts = async () => {
        if (!confirm(t('accountManager.testAllConfirm'))) return
        const allAccounts = config.accounts || []
        if (allAccounts.length === 0) return

        setTestingAll(true)
        setBatchProgress({ current: 0, total: allAccounts.length, results: [] })

        let successCount = 0
        const results = []

        for (let i = 0; i < allAccounts.length; i++) {
            const acc = allAccounts[i]
            const id = resolveAccountIdentifier(acc)
            if (!id) {
                results.push({ id: '-', success: false, message: t('accountManager.invalidIdentifier') })
                setBatchProgress({ current: i + 1, total: allAccounts.length, results: [...results] })
                continue
            }

            try {
                const res = await apiFetch('/admin/accounts/test', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ identifier: id }),
                })
                const data = await res.json()
                results.push({ id, success: data.success, message: data.message, time: data.response_time })
                if (data.success) successCount++
            } catch (e) {
                results.push({ id, success: false, message: e.message })
            }

            setBatchProgress({ current: i + 1, total: allAccounts.length, results: [...results] })
        }

        onMessage('success', t('accountManager.testAllCompleted', { success: successCount, total: allAccounts.length }))
        fetchAccounts() // 刷新当前页
        onRefresh()
        setTestingAll(false)
    }

    return (
        <div className="space-y-6">
            {/* Queue Status - Flat & Clean */}
            {
                queueStatus && (
                    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                        <div className="bg-card border border-border rounded-xl p-4 flex flex-col justify-between shadow-sm relative overflow-hidden group">
                            <div className="absolute right-0 top-0 p-4 opacity-5 group-hover:opacity-10 transition-opacity">
                                <CheckCircle2 className="w-16 h-16" />
                            </div>
                            <p className="text-xs font-medium text-muted-foreground uppercase tracking-widest">{t('accountManager.available')}</p>
                            <div className="mt-2 flex items-baseline gap-2">
                                <span className="text-3xl font-bold text-foreground">{queueStatus.available}</span>
                                <span className="text-xs text-muted-foreground">{t('accountManager.accountsUnit')}</span>
                            </div>
                        </div>
                        <div className="bg-card border border-border rounded-xl p-4 flex flex-col justify-between shadow-sm relative overflow-hidden group">
                            <div className="absolute right-0 top-0 p-4 opacity-5 group-hover:opacity-10 transition-opacity">
                                <Server className="w-16 h-16" />
                            </div>
                            <p className="text-xs font-medium text-muted-foreground uppercase tracking-widest">{t('accountManager.inUse')}</p>
                            <div className="mt-2 flex items-baseline gap-2">
                                <span className="text-3xl font-bold text-foreground">{queueStatus.in_use}</span>
                                <span className="text-xs text-muted-foreground">{t('accountManager.threadsUnit')}</span>
                            </div>
                        </div>
                        <div className="bg-card border border-border rounded-xl p-4 flex flex-col justify-between shadow-sm relative overflow-hidden group">
                            <div className="absolute right-0 top-0 p-4 opacity-5 group-hover:opacity-10 transition-opacity">
                                <ShieldCheck className="w-16 h-16" />
                            </div>
                            <p className="text-xs font-medium text-muted-foreground uppercase tracking-widest">{t('accountManager.totalPool')}</p>
                            <div className="mt-2 flex items-baseline gap-2">
                                <span className="text-3xl font-bold text-foreground">{queueStatus.total}</span>
                                <span className="text-xs text-muted-foreground">{t('accountManager.accountsUnit')}</span>
                            </div>
                        </div>
                    </div>
                )
            }

            {/* API Keys Section */}
            <div className="bg-card border border-border rounded-xl overflow-hidden shadow-sm">
                <div
                    className="p-6 flex flex-col md:flex-row md:items-center justify-between gap-4 cursor-pointer select-none hover:bg-muted/30 transition-colors"
                    onClick={() => setKeysExpanded(!keysExpanded)}
                >
                    <div className="flex items-center gap-3">
                        <ChevronDown className={clsx(
                            "w-5 h-5 text-muted-foreground transition-transform duration-200",
                            keysExpanded ? "rotate-0" : "-rotate-90"
                        )} />
                        <div>
                            <h2 className="text-lg font-semibold">{t('accountManager.apiKeysTitle')}</h2>
                            <p className="text-sm text-muted-foreground">{t('accountManager.apiKeysDesc')} ({config.keys?.length || 0})</p>
                        </div>
                    </div>
                    <button
                        onClick={(e) => { e.stopPropagation(); setShowAddKey(true) }}
                        className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors font-medium text-sm shadow-sm"
                    >
                        <Plus className="w-4 h-4" />
                        {t('accountManager.addKey')}
                    </button>
                </div>

                {keysExpanded && (
                    <div className="divide-y divide-border border-t border-border">
                        {config.keys?.length > 0 ? (
                            config.keys.map((key, i) => (
                                <div key={i} className="p-4 flex items-center justify-between hover:bg-muted/50 transition-colors group">
                                    <div className="flex items-center gap-2">
                                        <div className="font-mono text-sm bg-muted/50 px-3 py-1 rounded inline-block">
                                            {key.slice(0, 16)}****
                                        </div>
                                        {copiedKey === key && (
                                            <span className="text-xs text-green-500 animate-pulse">{t('accountManager.copied')}</span>
                                        )}
                                    </div>
                                    <div className="flex items-center gap-1">
                                        <button
                                            onClick={() => {
                                                navigator.clipboard.writeText(key)
                                                setCopiedKey(key)
                                                setTimeout(() => setCopiedKey(null), 2000)
                                            }}
                                            className="p-2 text-muted-foreground hover:text-primary hover:bg-primary/10 rounded-md transition-colors opacity-0 group-hover:opacity-100"
                                            title={t('accountManager.copyKeyTitle')}
                                        >
                                            {copiedKey === key ? <Check className="w-4 h-4 text-green-500" /> : <Copy className="w-4 h-4" />}
                                        </button>
                                        <button
                                            onClick={() => deleteKey(key)}
                                            className="p-2 text-muted-foreground hover:text-destructive hover:bg-destructive/10 rounded-md transition-colors opacity-0 group-hover:opacity-100"
                                            title={t('accountManager.deleteKeyTitle')}
                                        >
                                            <Trash2 className="w-4 h-4" />
                                        </button>
                                    </div>
                                </div>
                            ))
                        ) : (
                            <div className="p-8 text-center text-muted-foreground">{t('accountManager.noApiKeys')}</div>
                        )}
                    </div>
                )}
            </div>

            {/* Accounts Section */}
            <div className="bg-card border border-border rounded-xl overflow-hidden shadow-sm">
                <div className="p-6 border-b border-border flex flex-col md:flex-row md:items-center justify-between gap-4">
                    <div>
                        <h2 className="text-lg font-semibold">{t('accountManager.accountsTitle')}</h2>
                        <p className="text-sm text-muted-foreground">{t('accountManager.accountsDesc')}</p>
                    </div>
                    <div className="flex flex-wrap gap-2">
                        <button
                            onClick={testAllAccounts}
                            disabled={testingAll || totalAccounts === 0}
                            className="flex items-center px-3 py-2 bg-secondary text-secondary-foreground rounded-lg hover:bg-secondary/80 transition-colors text-xs font-medium border border-border disabled:opacity-50"
                        >
                            {testingAll ? <Loader2 className="w-3 h-3 mr-2 animate-spin" /> : <Play className="w-3 h-3 mr-2" />}
                            {t('accountManager.testAll')}
                        </button>
                        <button
                            onClick={() => setShowAddAccount(true)}
                            className="flex items-center gap-2 px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors font-medium text-sm shadow-sm"
                        >
                            <Plus className="w-4 h-4" />
                            {t('accountManager.addAccount')}
                        </button>
                    </div>
                </div>

                {/* Batch Progress */}
                {testingAll && batchProgress.total > 0 && (
                    <div className="p-4 border-b border-border bg-muted/30">
                        <div className="flex items-center justify-between text-sm mb-2">
                            <span className="font-medium">{t('accountManager.testingAllAccounts')}</span>
                            <span className="text-muted-foreground">{batchProgress.current} / {batchProgress.total}</span>
                        </div>
                        <div className="w-full bg-muted rounded-full h-2 overflow-hidden mb-4">
                            <div
                                className="bg-primary h-full transition-all duration-300"
                                style={{ width: `${(batchProgress.current / batchProgress.total) * 100}%` }}
                            />
                        </div>
                        {batchProgress.results.length > 0 && (
                            <div className="grid grid-cols-2 md:grid-cols-4 gap-2 max-h-32 overflow-y-auto custom-scrollbar">
                                {batchProgress.results.map((r, i) => (
                                    <div key={i} className={clsx(
                                        "text-xs px-2 py-1 rounded border truncate",
                                        r.success ? "bg-emerald-500/10 border-emerald-500/20 text-emerald-500" : "bg-destructive/10 border-destructive/20 text-destructive"
                                    )}>
                                        {r.success ? 'OK' : 'ERR'} {r.id}
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                )}

                <div className="divide-y divide-border">
                    {loadingAccounts ? (
                        <div className="p-8 text-center text-muted-foreground">{t('actions.loading')}</div>
                    ) : accounts.length > 0 ? (
                        accounts.map((acc, i) => {
                            const id = resolveAccountIdentifier(acc)
                            return (
                                <div key={i} className="p-4 flex flex-col md:flex-row md:items-center justify-between gap-4 hover:bg-muted/50 transition-colors">
                                    <div className="flex items-center gap-3 min-w-0">
                                        <div className={clsx(
                                            "w-2 h-2 rounded-full shrink-0",
                                            acc.has_token ? "bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]" : "bg-amber-500"
                                        )} />
                                        <div className="min-w-0">
                                            <div className="font-medium truncate">{id || '-'}</div>
                                            <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5">
                                                <span>{acc.has_token ? t('accountManager.sessionActive') : t('accountManager.reauthRequired')}</span>
                                                {acc.token_preview && (
                                                    <span className="font-mono bg-muted px-1.5 py-0.5 rounded text-[10px]">
                                                        {acc.token_preview}
                                                    </span>
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                    <div className="flex items-center gap-2 self-start lg:self-auto ml-5 lg:ml-0">
                                        <button
                                            onClick={() => testAccount(id)}
                                            disabled={testing[id]}
                                            className="px-2 lg:px-3 py-1 lg:py-1.5 text-[10px] lg:text-xs font-medium border border-border rounded-md hover:bg-secondary transition-colors disabled:opacity-50"
                                        >
                                            {testing[id] ? t('actions.testing') : t('actions.test')}
                                        </button>
                                        <button
                                            onClick={() => deleteAccount(id)}
                                            className="p-1 lg:p-1.5 text-muted-foreground hover:text-destructive hover:bg-destructive/10 rounded-md transition-colors"
                                        >
                                            <Trash2 className="w-3.5 h-3.5 lg:w-4 lg:h-4" />
                                        </button>
                                    </div>
                                </div>
                            )
                        })
                    ) : (
                        <div className="p-8 text-center text-muted-foreground">{t('accountManager.noAccounts')}</div>
                    )}
                </div>

                {/* 分页控件 */}
                {totalPages > 1 && (
                    <div className="p-4 border-t border-border flex items-center justify-between">
                        <div className="text-sm text-muted-foreground">
                            {t('accountManager.pageInfo', { current: page, total: totalPages, count: totalAccounts })}
                        </div>
                        <div className="flex items-center gap-2">
                            <button
                                onClick={() => fetchAccounts(page - 1)}
                                disabled={page <= 1 || loadingAccounts}
                                className="p-2 border border-border rounded-md hover:bg-secondary transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                <ChevronLeft className="w-4 h-4" />
                            </button>
                            <span className="text-sm font-medium px-2">{page} / {totalPages}</span>
                            <button
                                onClick={() => fetchAccounts(page + 1)}
                                disabled={page >= totalPages || loadingAccounts}
                                className="p-2 border border-border rounded-md hover:bg-secondary transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                                <ChevronRight className="w-4 h-4" />
                            </button>
                        </div>
                    </div>
                )}
            </div>

            {/* Modals */}
            {
                showAddKey && (
                    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in">
                        <div className="bg-card w-full max-w-md rounded-xl border border-border shadow-2xl overflow-hidden animate-in zoom-in-95">
                            <div className="p-4 border-b border-border flex justify-between items-center">
                                <h3 className="font-semibold">{t('accountManager.modalAddKeyTitle')}</h3>
                                <button onClick={() => setShowAddKey(false)} className="text-muted-foreground hover:text-foreground">
                                    <X className="w-5 h-5" />
                                </button>
                            </div>
                            <div className="p-6 space-y-4">
                                <div>
                                    <label className="block text-sm font-medium mb-1.5">{t('accountManager.newKeyLabel')}</label>
                                    <div className="flex gap-2">
                                        <input
                                            type="text"
                                            className="input-field bg-[#09090b] flex-1"
                                            placeholder={t('accountManager.newKeyPlaceholder')}
                                            value={newKey}
                                            onChange={e => setNewKey(e.target.value)}
                                            autoFocus
                                        />
                                        <button
                                            type="button"
                                            onClick={() => setNewKey('sk-' + crypto.randomUUID().replace(/-/g, ''))}
                                            className="px-3 py-2 bg-secondary text-secondary-foreground rounded-lg hover:bg-secondary/80 transition-colors text-sm font-medium border border-border whitespace-nowrap"
                                        >
                                            {t('accountManager.generate')}
                                        </button>
                                    </div>
                                    <p className="text-xs text-muted-foreground mt-1.5">{t('accountManager.generateHint')}</p>
                                </div>
                                <div className="flex justify-end gap-2 pt-2">
                                    <button onClick={() => setShowAddKey(false)} className="px-4 py-2 rounded-lg border border-border hover:bg-secondary transition-colors text-sm font-medium">{t('actions.cancel')}</button>
                                    <button onClick={addKey} disabled={loading} className="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors text-sm font-medium disabled:opacity-50">
                                        {loading ? t('accountManager.addKeyLoading') : t('accountManager.addKeyAction')}
                                    </button>
                                </div>
                            </div>
                        </div>
                    </div>
                )
            }

            {
                showAddAccount && (
                    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4 animate-in fade-in">
                        <div className="bg-card w-full max-w-md rounded-xl border border-border shadow-2xl overflow-hidden animate-in zoom-in-95">
                            <div className="p-4 border-b border-border flex justify-between items-center">
                                <h3 className="font-semibold">{t('accountManager.modalAddAccountTitle')}</h3>
                                <button onClick={() => setShowAddAccount(false)} className="text-muted-foreground hover:text-foreground">
                                    <X className="w-5 h-5" />
                                </button>
                            </div>
                            <div className="p-6 space-y-4">
                                <div>
                                    <label className="block text-sm font-medium mb-1.5">{t('accountManager.emailOptional')}</label>
                                    <input
                                        type="email"
                                        className="input-field"
                                        placeholder="user@example.com"
                                        value={newAccount.email}
                                        onChange={e => setNewAccount({ ...newAccount, email: e.target.value })}
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium mb-1.5">{t('accountManager.mobileOptional')}</label>
                                    <input
                                        type="text"
                                        className="input-field"
                                        placeholder="+86..."
                                        value={newAccount.mobile}
                                        onChange={e => setNewAccount({ ...newAccount, mobile: e.target.value })}
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium mb-1.5">{t('accountManager.passwordLabel')} <span className="text-destructive">*</span></label>
                                    <input
                                        type="password"
                                        className="input-field bg-[#09090b]"
                                        placeholder={t('accountManager.passwordPlaceholder')}
                                        value={newAccount.password}
                                        onChange={e => setNewAccount({ ...newAccount, password: e.target.value })}
                                    />
                                </div>
                                <div className="flex justify-end gap-2 pt-2">
                                    <button onClick={() => setShowAddAccount(false)} className="px-4 py-2 rounded-lg border border-border hover:bg-secondary transition-colors text-sm font-medium">{t('actions.cancel')}</button>
                                    <button onClick={addAccount} disabled={loading} className="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-colors text-sm font-medium disabled:opacity-50">
                                        {loading ? t('accountManager.addAccountLoading') : t('accountManager.addAccountAction')}
                                    </button>
                                </div>
                            </div>
                        </div>
                    </div>
                )
            }
        </div >
    )
}
