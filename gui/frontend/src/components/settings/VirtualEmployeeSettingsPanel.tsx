import { useCallback, useEffect, useRef, useState } from 'react';
import { EventsOn, EventsOff } from '../../../wailsjs/runtime';

// Wails bindings — these functions are defined in gui/app_ve.go.
// The generated App.js may not include them yet; the test mocks this module.
import { RegisterVirtualEmployee, UpdateVESettings, GetVEStatus, GetDigitalEmployeeSensitiveQueryPolicy, SaveDigitalEmployeeSensitiveQueryPolicy } from '../../../wailsjs/go/main/App';

// --- Types ---

type VEStatusResponse = {
    registered: boolean;
    employee?: {
        id?: string;
        name: string;
        skill_description: string;
        access_policy: string;
        status: string;
        online_status?: string;
        registered_at?: string;
    };
};

export type AccessPolicy = 'public' | 'whitelist' | 'blacklist' | 'per_request';
export type VEStatus = 'pending' | 'active' | 'disabled' | 'rejected';

type Props = {
    remoteMachineId: string;
    lang?: string;
};

// --- Constants ---

const STATUS_LABELS: Record<VEStatus, string> = {
    pending: '审核中',
    active: '已激活',
    disabled: '已禁用',
    rejected: '已拒绝',
};

const STATUS_COLORS: Record<VEStatus, string> = {
    pending: '#f59e0b',
    active: '#10b981',
    disabled: '#6b7280',
    rejected: '#ef4444',
};

const POLICY_OPTIONS: { value: AccessPolicy; label: string }[] = [
    { value: 'public', label: '所有人可访问' },
    { value: 'whitelist', label: '仅白名单可访问' },
    { value: 'blacklist', label: '黑名单不可访问' },
    { value: 'per_request', label: '每次访问需授权' },
];

// --- Component ---

export function VirtualEmployeeSettingsPanel({ remoteMachineId, lang }: Props) {
    const [name, setName] = useState('');
    const [skillDescription, setSkillDescription] = useState('');
    const [accessPolicy, setAccessPolicy] = useState<AccessPolicy | ''>('');
    const [whitelist, setWhitelist] = useState<string[]>([]);
    const [blacklist, setBlacklist] = useState<string[]>([]);
    const [status, setStatus] = useState<VEStatus | null>(null);
    const [registered, setRegistered] = useState(false);
    const [listInput, setListInput] = useState('');
    const [approvalNotice, setApprovalNotice] = useState('');
    const [sensitiveQueryPolicy, setSensitiveQueryPolicy] = useState<'confirm' | 'deny' | 'allow'>('confirm');

    const [nameError, setNameError] = useState('');
    const [skillError, setSkillError] = useState('');
    const [policyError, setPolicyError] = useState('');

    const mountedRef = useRef(true);

    // --- Load VE status on mount ---
    useEffect(() => {
        mountedRef.current = true;
        if (remoteMachineId) {
            loadStatus();
        }
        return () => { mountedRef.current = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [remoteMachineId]);

    useEffect(() => {
        GetDigitalEmployeeSensitiveQueryPolicy()
            .then((policy) => {
                if (policy === 'deny' || policy === 'allow' || policy === 'confirm') setSensitiveQueryPolicy(policy);
            })
            .catch(() => setSensitiveQueryPolicy('confirm'));
    }, []);

    const handleSensitiveQueryPolicyChange = useCallback(async (value: 'confirm' | 'deny' | 'allow') => {
        setSensitiveQueryPolicy(value);
        try {
            await SaveDigitalEmployeeSensitiveQueryPolicy(value);
        } catch {
            // Keep UI responsive; config save failures are surfaced by backend logs for now.
        }
    }, []);

    // --- Listen for ve:approved event (task 9.5) ---
    useEffect(() => {
        if (!remoteMachineId) return;
        const unsub = EventsOn('ve:approved', () => {
            loadStatus();
            if (mountedRef.current) {
                setApprovalNotice('🎉 您的数字员工注册已通过审批！');
                setTimeout(() => { if (mountedRef.current) setApprovalNotice(''); }, 8000);
            }
        });
        return () => {
            if (typeof unsub === 'function') unsub();
            else EventsOff('ve:approved');
        };
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [remoteMachineId]);

    // --- Conditional render: only show when remote_machine_id is non-empty ---
    if (!remoteMachineId) {
        return null;
    }

    async function loadStatus() {
        try {
            const resp = await GetVEStatus();
            if (!mountedRef.current) return;
            if (resp && resp.registered && resp.employee) {
                setRegistered(true);
                setName(resp.employee.name || '');
                setSkillDescription(resp.employee.skill_description || '');
                setAccessPolicy((resp.employee.access_policy as AccessPolicy) || '');
                setStatus((resp.employee.status as VEStatus) || null);
            }
        } catch {
            // ignore — GetVEStatus returns {registered: false} on error
        }
    }

    function validateName(value: string): string {
        if (!value.trim()) return '名称不能为空';
        if (value.length > 50) return '名称不能超过50个字符';
        return '';
    }

    function validateSkillDescription(value: string): string {
        if (!value.trim()) return '技能描述不能为空';
        if (value.length > 500) return '技能描述不能超过500个字符';
        return '';
    }

    function validatePolicy(value: string): string {
        if (!value) return '请选择访问策略';
        return '';
    }

    function validate(): boolean {
        const ne = validateName(name);
        const se = validateSkillDescription(skillDescription);
        const pe = validatePolicy(accessPolicy);
        setNameError(ne);
        setSkillError(se);
        setPolicyError(pe);
        return !ne && !se && !pe;
    }

    const handleSubmit = useCallback(async () => {
        if (!validate()) return;

        const list = accessPolicy === 'whitelist' ? whitelist : accessPolicy === 'blacklist' ? blacklist : [];

        try {
            if (registered) {
                await UpdateVESettings(name, skillDescription, accessPolicy, list);
            } else {
                await RegisterVirtualEmployee(name, skillDescription, accessPolicy, list);
                setRegistered(true);
                setStatus('pending');
            }
            // Reload status after successful submission
            await loadStatus();
        } catch {
            // error handling — could show a toast in the future
        }
    }, [name, skillDescription, accessPolicy, whitelist, blacklist, registered]);

    function handleAddToList() {
        const item = listInput.trim();
        if (!item) return;
        if (accessPolicy === 'whitelist') {
            if (!whitelist.includes(item)) {
                setWhitelist([...whitelist, item]);
            }
        } else if (accessPolicy === 'blacklist') {
            if (!blacklist.includes(item)) {
                setBlacklist([...blacklist, item]);
            }
        }
        setListInput('');
    }

    function handleRemoveFromList(item: string) {
        if (accessPolicy === 'whitelist') {
            setWhitelist(whitelist.filter(i => i !== item));
        } else if (accessPolicy === 'blacklist') {
            setBlacklist(blacklist.filter(i => i !== item));
        }
    }

    const showListEditor = accessPolicy === 'whitelist' || accessPolicy === 'blacklist';

    return (
        <div data-testid="ve-settings-panel">
            <h3>数字员工设置</h3>

            {/* --- Approval notification (task 9.5) --- */}
            {approvalNotice && (
                <div style={{ marginBottom: '10px', padding: '6px 10px', borderRadius: '4px', backgroundColor: '#10b98120', color: '#10b981', fontSize: '0.8rem' }}>
                    {approvalNotice}
                </div>
            )}

            {/* --- Status display (task 9.5) --- */}
            {status && (
                <div data-testid="ve-status-badge" style={{ color: STATUS_COLORS[status], marginBottom: '12px', fontWeight: 500 }}>
                    {STATUS_LABELS[status]}
                </div>
            )}

            {/* --- Name field (task 9.3) --- */}
            <div style={{ marginBottom: '10px' }}>
                <label htmlFor="ve-name">名称</label>
                <input
                    id="ve-name"
                    type="text"
                    value={name}
                    maxLength={50}
                    onChange={e => {
                        setName(e.target.value);
                        setNameError(validateName(e.target.value));
                    }}
                    placeholder="数字员工名称"
                />
                {nameError && <span data-testid="name-error" role="alert">{nameError}</span>}
            </div>

            {/* --- Skill description field (task 9.3) --- */}
            <div style={{ marginBottom: '10px' }}>
                <label htmlFor="ve-skill">技能描述</label>
                <textarea
                    id="ve-skill"
                    value={skillDescription}
                    maxLength={500}
                    onChange={e => {
                        setSkillDescription(e.target.value);
                        setSkillError(validateSkillDescription(e.target.value));
                    }}
                    placeholder="描述数字员工的技能和能力"
                />
                {skillError && <span data-testid="skill-error" role="alert">{skillError}</span>}
            </div>

            {/* --- Access policy selector (task 9.3) --- */}
            <div style={{ marginBottom: '10px' }}>
                <label htmlFor="ve-policy">访问策略</label>
                <select
                    id="ve-policy"
                    value={accessPolicy}
                    onChange={e => {
                        const val = e.target.value as AccessPolicy | '';
                        setAccessPolicy(val);
                        setPolicyError(validatePolicy(val));
                    }}
                >
                    <option value="">请选择</option>
                    {POLICY_OPTIONS.map(opt => (
                        <option key={opt.value} value={opt.value}>{opt.label}</option>
                    ))}
                </select>
                {policyError && <span data-testid="policy-error" role="alert">{policyError}</span>}
            </div>

            {/* --- Whitelist/Blacklist editor (task 9.4) --- */}
            {showListEditor && (
                <div data-testid="list-editor">
                    <label>{accessPolicy === 'whitelist' ? '白名单' : '黑名单'}</label>
                    <div style={{ display: 'flex', gap: '6px', marginBottom: '8px' }}>
                        <input
                            data-testid="list-input"
                            type="text"
                            value={listInput}
                            onChange={e => setListInput(e.target.value)}
                            onKeyDown={e => { if (e.key === 'Enter') handleAddToList(); }}
                            placeholder="输入用户标识"
                        />
                        <button onClick={handleAddToList} data-testid="list-add-btn">添加</button>
                    </div>
                    <ul data-testid="list-items">
                        {(accessPolicy === 'whitelist' ? whitelist : blacklist).map(item => (
                            <li key={item}>
                                {item}
                                <button onClick={() => handleRemoveFromList(item)} data-testid={`remove-${item}`}>移除</button>
                            </li>
                        ))}
                    </ul>
                </div>
            )}

            <div style={{ marginBottom: '14px', padding: '10px', border: '1px solid var(--theme-border)', borderRadius: '8px' }}>
                <label htmlFor="ve-sensitive-policy">{"\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2"}</label>
                <select
                    id="ve-sensitive-policy"
                    value={sensitiveQueryPolicy}
                    onChange={e => handleSensitiveQueryPolicyChange(e.target.value as 'confirm' | 'deny' | 'allow')}
                >
                    <option value="confirm">{"\u4eba\u5de5\u786e\u8ba4"}</option>
                    <option value="deny">{"\u62d2\u7edd"}</option>
                    <option value="allow">{"\u81ea\u52a8\u5141\u8bb8"}</option>
                </select>
                <div style={{ marginTop: '6px', fontSize: '12px', color: 'var(--theme-text-muted)' }}>
                    {"\u9ed8\u8ba4\u4eba\u5de5\u786e\u8ba4\u3002\u9009\u62e9\u4eba\u5de5\u786e\u8ba4\u65f6\uff0c\u6570\u5b57\u5458\u5de5\u9047\u5230\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2\u4f1a\u7b49\u5f85\u672c\u5730\u4eba\u7c7b\u5458\u5de5\u8bb8\u53ef\uff0c1 \u5206\u949f\u65e0\u54cd\u5e94\u5219\u9ed8\u8ba4\u62d2\u7edd\u3002"}
                </div>
            </div>

            {/* --- Submit button --- */}
            <button onClick={handleSubmit} data-testid="ve-submit-btn">
                {registered ? '更新设置' : '注册数字员工'}
            </button>
        </div>
    );
}
