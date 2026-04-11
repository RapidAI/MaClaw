import { useCallback, useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

/* ── Types ── */
type Policy = { id: string; name: string; policy_type: string; scope: string; priority: number; status: string };
type HitRecord = { id: string; policy_name: string; actor_id: string; action: string; detail: string; created_at: string };
type TreeNode = { id: string; name: string; parent_id: string; member_count: number; children?: TreeNode[] };
type Settings = { centralized_security_enabled: boolean; org_structure_enabled: boolean; default_group_id?: string };
type PolicyItemView = { value: unknown; source: string; source_group: string; source_name: string };
type GroupPolicyView = { group_id: string; items: Record<string, PolicyItemView> };

const API = '/admin/security';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try { const r = await fetch(url); if (!r.ok) return null; return r.json(); } catch { return null; }
}
async function postJSON(url: string, body: unknown) {
  return fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
}
async function putJSON(url: string, body: unknown) {
  return fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
}
async function del(url: string) { return fetch(url, { method: 'DELETE' }); }

const POLICY_FIELDS = [
  { key: 'file_outbound_enabled', label: '文件外发', type: 'bool' },
  { key: 'image_outbound_enabled', label: '图片外发', type: 'bool' },
  { key: 'gossip_enabled', label: '吐槽墙', type: 'bool' },
  { key: 'guardrail_mode', label: '护栏模式', type: 'select', options: ['standard', 'strict', 'permissive'] },
  { key: 'sandbox_mode', label: '沙箱模式', type: 'select', options: ['none', 'docker', 'vm'] },
  { key: 'network_level', label: '网络级别', type: 'select', options: ['full', 'restricted', 'none'] },
  { key: 'yolo_mode_allowed', label: 'YOLO 模式', type: 'bool' },
  { key: 'smart_route_enabled', label: '智能路由', type: 'bool' },
];

export function SecurityPage() {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [hits, setHits] = useState<HitRecord[]>([]);
  const [tree, setTree] = useState<TreeNode | null>(null);
  const [settings, setSettings] = useState<Settings>({ centralized_security_enabled: false, org_structure_enabled: false });
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [groupPolicy, setGroupPolicy] = useState<GroupPolicyView | null>(null);
  const [groupMembers, setGroupMembers] = useState<string[]>([]);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [toast, setToast] = useState('');
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; groupId: string } | null>(null);

  const showToast = (msg: string) => { setToast(msg); setTimeout(() => setToast(''), 2500); };

  const loadAll = useCallback(async () => {
    fetchJSON<{ policies: Policy[] }>(`${API}/policies`).then(d => { if (d?.policies) setPolicies(d.policies); });
    fetchJSON<{ hits: HitRecord[] }>(`${API}/hits`).then(d => { if (d?.hits) setHits(d.hits.slice(0, 20)); });
    fetchJSON<{ tree: TreeNode }>(`${API}/groups`).then(d => { if (d?.tree) setTree(d.tree); });
    fetchJSON<Settings>(`${API}/settings`).then(d => { if (d) setSettings(d); });
  }, []);

  useEffect(() => { loadAll(); }, [loadAll]);

  const loadGroupDetail = async (gid: string) => {
    setSelectedGroupId(gid);
    const pv = await fetchJSON<GroupPolicyView>(`${API}/groups/${gid}/policy`);
    if (pv) setGroupPolicy(pv);
    const md = await fetchJSON<{ members: string[] }>(`${API}/groups/${gid}/members`);
    if (md) setGroupMembers(md.members || []);
  };

  /* ── Settings toggles ── */
  const toggleCentralized = async (checked: boolean) => {
    const next = { ...settings, centralized_security_enabled: checked };
    const r = await putJSON(`${API}/settings`, next);
    if (r.ok) { setSettings(next); showToast('集中管控已' + (checked ? '开启' : '关闭')); }
  };
  const toggleOrg = async (checked: boolean) => {
    const next = { ...settings, org_structure_enabled: checked };
    const r = await putJSON(`${API}/settings`, next);
    if (r.ok) { setSettings(next); showToast('组织机构已' + (checked ? '开启' : '关闭')); }
  };

  /* ── Group tree actions ── */
  const createSubGroup = async (parentId: string) => {
    const name = prompt('请输入子组名称：');
    if (!name?.trim()) return;
    const r = await postJSON(`${API}/groups`, { name: name.trim(), parent_id: parentId });
    if (r.ok) { setExpanded(e => ({ ...e, [parentId]: true })); loadAll(); showToast('子组已创建'); }
  };
  const renameGroup = async (gid: string) => {
    const name = prompt('请输入新名称：');
    if (!name?.trim()) return;
    const r = await putJSON(`${API}/groups/${gid}`, { name: name.trim() });
    if (r.ok) { loadAll(); showToast('已重命名'); }
  };
  const deleteGroup = async (gid: string) => {
    if (!confirm('确定要删除此用户组？\n该组及其所有子组中的用户将被移回全局组。')) return;
    const r = await del(`${API}/groups/${gid}`);
    if (r.ok) { if (selectedGroupId === gid) { setSelectedGroupId(null); setGroupPolicy(null); } loadAll(); showToast('用户组已删除'); }
  };
  const assignUser = async (gid: string) => {
    const email = prompt('请输入用户邮箱：');
    if (!email?.trim()) return;
    const r = await postJSON(`${API}/groups/${gid}/members`, { email: email.trim() });
    if (r.ok) { loadAll(); if (selectedGroupId === gid) loadGroupDetail(gid); showToast('用户已分配'); }
  };
  const removeUser = async (gid: string, email: string) => {
    const r = await del(`${API}/groups/${gid}/members/${encodeURIComponent(email)}`);
    if (r.ok) { loadAll(); loadGroupDetail(gid); showToast('用户已移除'); }
  };

  /* ── Save group policy ── */
  const savePolicyEdits = async () => {
    if (!selectedGroupId || !groupPolicy) return;
    const sparse: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(groupPolicy.items)) {
      if (v.source === 'self') sparse[k] = v.value;
    }
    const r = await putJSON(`${API}/groups/${selectedGroupId}/policy`, { policy: sparse });
    if (r.ok) { showToast('策略已保存'); loadGroupDetail(selectedGroupId); }
  };

  const setPolicyField = (key: string, value: unknown) => {
    if (!groupPolicy) return;
    setGroupPolicy({
      ...groupPolicy,
      items: { ...groupPolicy.items, [key]: { ...groupPolicy.items[key], value, source: 'self', source_group: groupPolicy.group_id, source_name: '' } },
    });
  };

  /* ── Context menu ── */
  useEffect(() => {
    const handler = () => setCtxMenu(null);
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  /* ── Render tree node ── */
  const renderTreeNode = (node: TreeNode, depth: number): JSX.Element => {
    const isExpanded = expanded[node.id] !== false;
    const hasChildren = node.children && node.children.length > 0;
    const isSelected = node.id === selectedGroupId;
    return (
      <div key={node.id}>
        <div
          style={{
            padding: '4px 8px 4px ' + (depth * 18 + 8) + 'px',
            borderRadius: 8, display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer',
            background: isSelected ? 'rgba(47,128,237,.12)' : undefined, fontWeight: isSelected ? 700 : undefined,
          }}
          onClick={() => loadGroupDetail(node.id)}
          onContextMenu={e => { e.preventDefault(); setCtxMenu({ x: e.clientX, y: e.clientY, groupId: node.id }); }}
        >
          <span
            style={{ fontSize: 10, width: 14, flexShrink: 0, textAlign: 'center', cursor: hasChildren ? 'pointer' : undefined }}
            onClick={e => { e.stopPropagation(); if (hasChildren) setExpanded(ex => ({ ...ex, [node.id]: !isExpanded })); }}
          >
            {hasChildren ? (isExpanded ? '▼' : '▶') : ' '}
          </span>
          <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{node.name}</span>
          <span style={{ fontSize: 11, color: '#888', flexShrink: 0 }}>{node.member_count || 0}</span>
        </div>
        {hasChildren && isExpanded && node.children!.map(c => renderTreeNode(c, depth + 1))}
      </div>
    );
  };

  /* ── Find group name ── */
  const findGroupName = (node: TreeNode | null, id: string): string => {
    if (!node) return id;
    if (node.id === id) return node.name;
    for (const c of node.children || []) { const r = findGroupName(c, id); if (r !== id) return r; }
    return id;
  };

  /* ── Render ── */
  const policyRows = policies.map(p => ({ name: p.name, type: p.policy_type, scope: p.scope, priority: String(p.priority), status: p.status }));
  const hitRows = hits.map(h => ({ policy: h.policy_name, action: h.action, actor: h.actor_id || '-', detail: h.detail.length > 40 ? h.detail.slice(0, 40) + '...' : h.detail, time: h.created_at }));

  return (
    <div className="center-page-stack">
      {toast && <div style={{ position: 'fixed', top: 18, right: 18, zIndex: 9999, padding: '12px 18px', borderRadius: 14, background: '#2c5f96', color: '#fff', boxShadow: '0 8px 24px rgba(0,0,0,.15)', fontSize: 13 }}>{toast}</div>}

      {/* Context menu */}
      {ctxMenu && (
        <div style={{ position: 'fixed', left: ctxMenu.x, top: ctxMenu.y, zIndex: 9999, background: '#fff', border: '1px solid #ddd', borderRadius: 10, boxShadow: '0 8px 24px rgba(0,0,0,.15)', padding: '6px 0', minWidth: 150 }}>
          <div style={{ padding: '8px 16px', cursor: 'pointer', fontSize: 13 }} onMouseDown={() => { setCtxMenu(null); createSubGroup(ctxMenu.groupId); }}>创建子组</div>
          <div style={{ padding: '8px 16px', cursor: 'pointer', fontSize: 13 }} onMouseDown={() => { setCtxMenu(null); renameGroup(ctxMenu.groupId); }}>重命名</div>
          <div style={{ padding: '8px 16px', cursor: 'pointer', fontSize: 13 }} onMouseDown={() => { setCtxMenu(null); assignUser(ctxMenu.groupId); }}>分配用户</div>
          <div style={{ padding: '8px 16px', cursor: 'pointer', fontSize: 13, color: '#c44' }} onMouseDown={() => { setCtxMenu(null); deleteGroup(ctxMenu.groupId); }}>删除</div>
        </div>
      )}

      {/* Settings bar */}
      <SectionCard title="安全管理" desc="管理用户组、安全策略和集中管控设置。">
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 14 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 14 }}>
            <input type="checkbox" checked={settings.centralized_security_enabled} onChange={e => toggleCentralized(e.target.checked)} />
            集中管控
            <span style={{ fontSize: 12, color: '#888' }}>{settings.centralized_security_enabled ? '已开启' : '已关闭'}</span>
          </label>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 14 }}>
            <input type="checkbox" checked={settings.org_structure_enabled} onChange={e => toggleOrg(e.target.checked)} />
            组织机构
            <span style={{ fontSize: 12, color: '#888' }}>{settings.org_structure_enabled ? '已开启' : '已关闭'}</span>
          </label>
          <div style={{ fontSize: 14, display: 'flex', alignItems: 'center', gap: 8 }}>
            默认组：<span style={{ color: '#555' }}>{settings.default_group_id ? findGroupName(tree, settings.default_group_id) : '未设置'}</span>
          </div>
        </div>
      </SectionCard>

      {/* Group tree + policy panel */}
      <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: 16, alignItems: 'start' }}>
        <SectionCard title="用户组" desc="右键点击组可创建子组、重命名、分配用户或删除。">
          <div style={{ fontSize: 13, lineHeight: 1.8, minHeight: 300 }}>
            {tree ? renderTreeNode(tree, 0) : <p style={{ color: '#888' }}>加载中...</p>}
          </div>
          <button onClick={loadAll} style={{ marginTop: 8, padding: '6px 14px', borderRadius: 8, border: '1px solid #ccc', background: '#f8f8f8', cursor: 'pointer', fontSize: 12 }}>刷新</button>
        </SectionCard>

        <div>
          {selectedGroupId && groupPolicy ? (
            <SectionCard title={'策略配置 — ' + findGroupName(tree, selectedGroupId)} desc={'组 ID: ' + selectedGroupId}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                {POLICY_FIELDS.map(f => {
                  const item = groupPolicy.items[f.key];
                  if (!item) return null;
                  return (
                    <div key={f.key} style={{ padding: 10, borderRadius: 10, border: '1px solid #eee', background: '#fafbff' }}>
                      <div style={{ fontSize: 12, fontWeight: 700, color: '#666', marginBottom: 4 }}>{f.label}</div>
                      {f.type === 'bool' ? (
                        <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13 }}>
                          <input type="checkbox" checked={!!item.value} onChange={e => setPolicyField(f.key, e.target.checked)} />
                          {item.value ? '允许' : '禁止'}
                        </label>
                      ) : (
                        <select value={String(item.value)} onChange={e => setPolicyField(f.key, e.target.value)} style={{ width: '100%', padding: '4px 8px', borderRadius: 6, border: '1px solid #ccc' }}>
                          {f.options?.map(o => <option key={o} value={o}>{o}</option>)}
                        </select>
                      )}
                      <div style={{ fontSize: 11, color: '#999', marginTop: 4 }}>
                        {item.source === 'self' ? '本组设置' : `继承自 ${item.source_name || item.source_group}`}
                      </div>
                    </div>
                  );
                })}
              </div>
              <button onClick={savePolicyEdits} style={{ marginTop: 12, padding: '8px 18px', borderRadius: 10, border: 'none', background: 'linear-gradient(135deg,#2c6fca,#558fd7)', color: '#fff', fontWeight: 700, cursor: 'pointer' }}>保存策略</button>

              {/* Group members */}
              <div style={{ marginTop: 18, borderTop: '1px solid #eee', paddingTop: 14 }}>
                <div style={{ fontWeight: 700, fontSize: 14, marginBottom: 8 }}>组成员 ({groupMembers.length})</div>
                {groupMembers.length === 0 ? (
                  <p style={{ color: '#888', fontSize: 13 }}>暂无成员</p>
                ) : (
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                    {groupMembers.map(email => (
                      <span key={email} style={{ padding: '4px 10px', borderRadius: 8, background: '#eef3ff', fontSize: 12, display: 'flex', alignItems: 'center', gap: 4 }}>
                        {email}
                        <span style={{ cursor: 'pointer', color: '#c44', fontWeight: 700 }} onClick={() => removeUser(selectedGroupId, email)}>×</span>
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </SectionCard>
          ) : (
            <SectionCard title="策略配置" desc="选择左侧用户组查看策略">
              <p style={{ color: '#888', padding: '40px 0', textAlign: 'center' }}>← 点击左侧用户组查看和编辑策略</p>
            </SectionCard>
          )}
        </div>
      </div>

      {/* Existing policy rules table */}
      <SectionCard title="安全策略规则" desc="管理关键词拦截、模型限制等安全规则。">
        <DataTable columns={[{ key: 'name', label: '策略名称' }, { key: 'type', label: '类型' }, { key: 'scope', label: '作用范围' }, { key: 'priority', label: '优先级' }, { key: 'status', label: '状态' }]} rows={policyRows} />
        {policies.length === 0 && <p style={{ color: '#888', padding: '8px 0' }}>暂无安全策略，可通过 API 创建。</p>}
      </SectionCard>

      <SectionCard title="策略命中记录" desc="最近的安全策略触发记录。">
        <DataTable columns={[{ key: 'policy', label: '策略' }, { key: 'action', label: '动作' }, { key: 'actor', label: '触发者' }, { key: 'detail', label: '详情' }, { key: 'time', label: '时间' }]} rows={hitRows} />
        {hits.length === 0 && <p style={{ color: '#888', padding: '8px 0' }}>暂无命中记录。</p>}
      </SectionCard>
    </div>
  );
}
