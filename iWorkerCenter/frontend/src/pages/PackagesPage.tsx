import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';

type Capability = {
  id: string;
  name: string;
  description: string;
  category: string;
  version: string;
  source: string;
  risk_level: string;
  status: string;
  package_status?: string;
};
type CloudMarketSkill = {
  id: string;
  name: string;
  description?: string;
  category?: string;
  version?: string;
  tags?: string[];
  risk_level?: string;
  status?: string;
  price?: number;
  source_center_id?: string;
  package_sha256?: string;
  package_size?: number;
  downloads?: number;
  download_count?: number;
};
type Colleague = { id: string; name: string; role_name?: string; role_code?: string; status?: string };
type CapabilityBinding = {
  id: string;
  colleague_id: string;
  colleague_name?: string;
  capability_id: string;
  capability_name?: string;
  bound_at: string;
};
type CapabilityBindResult = {
  status?: string;
  binding_id?: string;
  bound_at?: string;
};
type CapabilityUnbindResult = {
  status?: string;
  removed?: number;
};
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };
type WorkerDeliverySummary = {
  worker: Colleague;
  delivered: CapabilityBinding[];
};
type MCPServer = {
  id: string;
  name: string;
  description: string;
  server_type: 'http' | 'sse' | 'stdio' | string;
  endpoint: string;
  command?: string;
  args?: string[];
  env_keys?: string[];
  department_id: string;
  risk_level: string;
  status: string;
  installed_at?: string;
};
type MCPDraft = {
  name: string;
  description: string;
  server_type: 'http' | 'sse' | 'stdio';
  endpoint: string;
  command: string;
  args: string;
  env_keys: string;
  department_id: string;
  risk_level: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  let data: any = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { message: text };
    }
  }
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || `Request failed: ${resp.status}`);
  return data as T;
}

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    return await requestJSON<T>(url);
  } catch {
    return null;
  }
}

const toneForRisk = (risk: string) => risk === 'high' ? 'warn' : 'info';
const canApprove = (cap: Capability) => cap.status === 'pending_review' || cap.status === 'active';
const canReject = (cap: Capability) => cap.status === 'pending_review';
const canInstall = (cap: Capability) => (cap.status === 'active' || cap.status === 'approved') && cap.package_status === 'package_cached';
const isRuntimeInstalled = (cap: Capability) => cap.package_status === 'installed';
const canDeliver = (cap: Capability) => (cap.status === 'active' || cap.status === 'approved') && isRuntimeInstalled(cap);
const emptyMCPDraft: MCPDraft = { name: '', description: '', server_type: 'http', endpoint: '', command: '', args: '', env_keys: '', department_id: 'all', risk_level: 'medium' };
const splitList = (value: string) => value.split(/[\n,]/).map(item => item.trim()).filter(Boolean);

export function PackagesPage() {
  const { t } = useI18n();
  const [caps, setCaps] = useState<Capability[]>([]);
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [bindings, setBindings] = useState<CapabilityBinding[]>([]);
  const [mcpServers, setMCPServers] = useState<MCPServer[]>([]);
  const [mcpDraft, setMCPDraft] = useState<MCPDraft>(emptyMCPDraft);
  const [selectedWorker, setSelectedWorker] = useState<Record<string, string>>({});
  const [packageQuery, setPackageQuery] = useState('');
  const [cloudQuery, setCloudQuery] = useState('');
  const [cloudResults, setCloudResults] = useState<CloudMarketSkill[]>([]);
  const [loading, setLoading] = useState(false);
  const [cloudSearching, setCloudSearching] = useState(false);
  const [importingSkillID, setImportingSkillID] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message | null>(null);

  const cloudMarketLabel = t('Cloud 市场', 'Cloud Market');
  const sourceLabel = (source?: string) => {
    if (!source) return t('本地', 'Local');
    if (source.startsWith('hubcenter:') || source.startsWith('cloud:') || source.startsWith('iworkercloud:')) return cloudMarketLabel;
    if (source.startsWith('center:')) return t('Center 管理', 'Center Managed');
    return t('本地', 'Local');
  };
  const statusLabel = (status: string) => ({
    active: t('启用', 'Active'),
    approved: t('已批准', 'Approved'),
    pending_review: t('待审核', 'Pending review'),
    rejected: t('已拒绝', 'Rejected'),
    disabled: t('停用', 'Disabled'),
  }[status] || status || t('未知', 'Unknown'));
  const riskLabel = (risk: string) => ({
    low: t('低', 'Low'),
    medium: t('中', 'Medium'),
    high: t('高', 'High'),
  }[risk] || risk || t('低', 'Low'));
  const packageStatusLabel = (status?: string) => ({
    package_cached: t('包已缓存', 'Package cached'),
    package_unavailable: t('Cloud 包不可用', 'Cloud package unavailable'),
    metadata_only: t('仅元数据', 'Metadata only'),
    installed: t('已安装', 'Installed'),
  }[status || ''] || status || t('未安装', 'Not installed'));

  const loadCaps = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const data = await fetchJSON<{ capabilities: Capability[] }>('/admin/capabilities');
      if (data?.capabilities) {
        setCaps(data.capabilities);
      } else if (hasWails()) {
        const list = await (window as any).go.main.App.ListCapabilities();
        if (Array.isArray(list)) setCaps(list);
      }

      const colleaguesResp = await fetchJSON<{ colleagues?: Colleague[] }>('/admin/colleagues');
      if (Array.isArray(colleaguesResp?.colleagues)) {
        setColleagues(colleaguesResp.colleagues);
      } else if (hasWails()) {
        const list = await (window as any).go.main.App.ListColleagues();
        if (Array.isArray(list)) setColleagues(list);
      }

      const mcpResp = await fetchJSON<{ mcp_servers?: MCPServer[] }>('/admin/mcp-servers');
      if (Array.isArray(mcpResp?.mcp_servers)) setMCPServers(mcpResp.mcp_servers);

      const bindingResp = await fetchJSON<{ bindings?: CapabilityBinding[] }>('/admin/capability-bindings');
      setBindings(Array.isArray(bindingResp?.bindings) ? bindingResp.bindings : []);
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载能力包失败。', 'Failed to load capability packages.') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void loadCaps(); }, []);

  const summary = useMemo(() => {
    const pending = caps.filter(c => c.status === 'pending_review').length;
    const active = caps.filter(c => ['active', 'approved'].includes(c.status)).length;
    const highRisk = caps.filter(c => c.risk_level === 'high').length;
    const cloudItems = caps.filter(c => sourceLabel(c.source) === cloudMarketLabel).length;
    const installed = caps.filter(c => c.package_status === 'installed').length;
    const delivered = bindings.length;
    const enabledMCP = mcpServers.filter(server => server.status === 'enabled').length;
    return { pending, active, highRisk, cloudItems, installed, delivered, enabledMCP };
  }, [bindings.length, caps, cloudMarketLabel, mcpServers]);

  const bindingsByCapability = useMemo(() => {
    const map = new Map<string, CapabilityBinding[]>();
    for (const binding of bindings) {
      const list = map.get(binding.capability_id) || [];
      list.push(binding);
      map.set(binding.capability_id, list);
    }
    return map;
  }, [bindings]);

  const workerDeliverySummary = useMemo<WorkerDeliverySummary[]>(() => {
    return colleagues.map(worker => ({
      worker,
      delivered: bindings
        .filter(binding => binding.colleague_id === worker.id)
        .sort((a, b) => (a.capability_name || a.capability_id).localeCompare(b.capability_name || b.capability_id)),
    }));
  }, [bindings, colleagues]);

  const packageMatchesQuery = (cap: Capability) => {
    const query = packageQuery.trim().toLowerCase();
    if (!query) return true;
    const delivered = (bindingsByCapability.get(cap.id) || [])
      .map(binding => binding.colleague_name || binding.colleague_id)
      .join(' ');
    return [
      cap.id,
      cap.name,
      cap.description,
      cap.category,
      cap.version,
      cap.source,
      cap.risk_level,
      cap.status,
      cap.package_status,
      delivered,
    ].filter(Boolean).join(' ').toLowerCase().includes(query);
  };

  const mcpMatchesQuery = (server: MCPServer) => {
    const query = packageQuery.trim().toLowerCase();
    if (!query) return true;
    return [
      server.id,
      server.name,
      server.description,
      server.server_type,
      server.endpoint,
      server.command,
      server.department_id,
      server.risk_level,
      server.status,
      ...(server.args || []),
      ...(server.env_keys || []),
    ].filter(Boolean).join(' ').toLowerCase().includes(query);
  };

  const searchCloudMarket = async () => {
    setCloudSearching(true);
    setMessage(null);
    try {
      const data = await requestJSON<{ results?: CloudMarketSkill[] }>(`/admin/capabilities-import/search?q=${encodeURIComponent(cloudQuery.trim())}`);
      setCloudResults(Array.isArray(data?.results) ? data.results : []);
      if (!data?.results?.length) {
        setMessage({ kind: 'warn', text: t('Cloud 市场没有匹配的 Skill，或当前授权未包含能力市场。', 'No matching Skill was found in Cloud Market, or the current license does not include the capability market.') });
      }
    } catch (err) {
      setCloudResults([]);
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('搜索 Cloud 市场失败。', 'Cloud Market search failed.') });
    } finally {
      setCloudSearching(false);
    }
  };

  const importCloudSkill = async (skill: CloudMarketSkill) => {
    if (!skill.id) {
      setMessage({ kind: 'warn', text: t('Cloud Skill 缺少 ID，无法导入。', 'Cloud Skill has no ID and cannot be imported.') });
      return;
    }
    setImportingSkillID(skill.id);
    setMessage(null);
    try {
      const cap = await requestJSON<Capability>('/admin/capabilities-import/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ skill_id: skill.id }),
      });
      setMessage({ kind: 'ok', text: `${cap.name || skill.name || skill.id} ${t('已导入，审核后即可安装并下发。', 'imported. Review it, then install and deliver it.')}` });
      await loadCaps();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('导入 Cloud Skill 失败。', 'Cloud Skill import failed.') });
    } finally {
      setImportingSkillID('');
    }
  };

  const runAction = async (cap: Capability, action: 'approve' | 'reject' | 'install') => {
    const labels = { approve: t('已批准', 'approved'), reject: t('已拒绝', 'rejected'), install: t('已安装', 'installed') };
    setBusy(`${cap.id}:${action}`);
    setMessage(null);
    try {
      await requestJSON(`/admin/capabilities/${encodeURIComponent(cap.id)}/${encodeURIComponent(action)}`, { method: 'POST' });
      setMessage({ kind: 'ok', text: `${cap.name} ${labels[action]}.` });
      await loadCaps();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('操作失败。', 'Action failed.') });
    } finally {
      setBusy('');
    }
  };

  const bindToWorker = async (cap: Capability) => {
    const colleagueID = selectedWorker[cap.id] || colleagues[0]?.id || '';
    if (!colleagueID) {
      setMessage({ kind: 'warn', text: t('请先创建或选择一个 iWorker。', 'Create or select an iWorker first.') });
      return;
    }
    setBusy(`${cap.id}:bind`);
    setMessage(null);
    try {
      const result = await requestJSON<CapabilityBindResult>(`/admin/capabilities/${encodeURIComponent(cap.id)}/bind`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ colleague_id: colleagueID }),
      });
      const worker = colleagues.find(c => c.id === colleagueID);
      const alreadyBound = result?.status === 'already_bound';
      setMessage({
        kind: 'ok',
        text: alreadyBound
          ? cap.name + t(' 此前已下发给 ', ' was already delivered to ') + (worker?.name || colleagueID) + '.'
          : t('已下发 ', 'Delivered ') + cap.name + t(' 给 ', ' to ') + (worker?.name || colleagueID) + '.',
      });
      await loadCaps();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('下发失败。', 'Delivery failed.') });
    } finally {
      setBusy('');
    }
  };

  const unbindFromWorker = async (cap: Capability, binding: CapabilityBinding) => {
    const colleagueID = binding.colleague_id;
    if (!colleagueID) {
      return;
    }
    setBusy(`${cap.id}:unbind:${colleagueID}`);
    setMessage(null);
    try {
      const result = await requestJSON<CapabilityUnbindResult>(`/admin/capabilities/${encodeURIComponent(cap.id)}/unbind`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ colleague_id: colleagueID }),
      });
      const target = binding.colleague_name || colleagueID;
      setMessage({
        kind: result?.status === 'not_bound' ? 'warn' : 'ok',
        text: result?.status === 'not_bound'
          ? cap.name + t(' 未下发给 ', ' was not delivered to ') + target + '.'
          : t('已撤回 ', 'Revoked ') + cap.name + t(' 从 ', ' from ') + target + '.',
      });
      await loadCaps();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('撤回下发失败。', 'Delivery revoke failed.') });
    } finally {
      setBusy('');
    }
  };

  const createMCPServer = async () => {
    const name = mcpDraft.name.trim();
    const endpoint = mcpDraft.endpoint.trim();
    const command = mcpDraft.command.trim();
    if (!name) {
      setMessage({ kind: 'warn', text: t('请填写 MCP 名称。', 'Enter an MCP name.') });
      return;
    }
    if (mcpDraft.server_type === 'stdio' ? !command : !endpoint) {
      setMessage({
        kind: 'warn',
        text: mcpDraft.server_type === 'stdio'
          ? t('请填写 STDIO 命令。', 'Enter the STDIO command.')
          : t('请填写 MCP 服务端点。', 'Enter the MCP endpoint.'),
      });
      return;
    }
    setBusy('mcp:create');
    setMessage(null);
    try {
      await requestJSON<MCPServer>('/admin/mcp-servers', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name,
          description: mcpDraft.description.trim(),
          server_type: mcpDraft.server_type,
          endpoint,
          command,
          args: splitList(mcpDraft.args),
          env_keys: splitList(mcpDraft.env_keys),
          department_id: mcpDraft.department_id.trim() || 'all',
          risk_level: mcpDraft.risk_level,
          status: 'enabled',
        }),
      });
      setMCPDraft(emptyMCPDraft);
      setMessage({ kind: 'ok', text: t('MCP 服务已安装并启用。', 'MCP server installed and enabled.') });
      await loadCaps();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('MCP 服务安装失败。', 'MCP server installation failed.') });
    } finally {
      setBusy('');
    }
  };

  const setMCPStatus = async (server: MCPServer, status: 'enabled' | 'disabled') => {
    setBusy(`mcp:${server.id}:${status}`);
    setMessage(null);
    try {
      await requestJSON<MCPServer>(`/admin/mcp-servers/${encodeURIComponent(server.id)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: server.name,
          description: server.description,
          server_type: server.server_type,
          endpoint: server.endpoint,
          command: server.command || '',
          args: server.args || [],
          env_keys: server.env_keys || [],
          department_id: server.department_id || 'all',
          risk_level: server.risk_level || 'medium',
          status,
        }),
      });
      setMessage({ kind: 'ok', text: status === 'enabled' ? t('MCP 服务已启用。', 'MCP server enabled.') : t('MCP 服务已停用。', 'MCP server disabled.') });
      await loadCaps();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('MCP 状态更新失败。', 'MCP status update failed.') });
    } finally {
      setBusy('');
    }
  };

  const pendingCaps = caps.filter(c => c.status === 'pending_review' && packageMatchesQuery(c));
  const readyCaps = caps.filter(c => c.status !== 'pending_review' && packageMatchesQuery(c));
  const filteredMCPServers = mcpServers.filter(mcpMatchesQuery);

  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('能力包与 MCP', 'Capability Packages and MCP')}
        desc={t(
          'iWorkerCenter 负责安装、审核并向企业 iWorker 下发 Skill/MCP。iWorkerCloud 只作为市场和授权来源，不参与企业业务执行。',
          'iWorkerCenter installs, reviews, and delivers enterprise Skill/MCP packages. iWorkerCloud is only a market and authorization source and does not participate in enterprise work execution.',
        )}
      >
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label={t('可用', 'Available')} value={String(summary.active)} tone="ok" />
          <StatusTile label={t('待审核', 'Pending')} value={String(summary.pending)} tone={summary.pending ? 'warn' : 'ok'} />
          <StatusTile label={t('已安装运行时', 'Installed runtime')} value={String(summary.installed)} tone="ok" />
          <StatusTile label={t('已下发', 'Delivered')} value={String(summary.delivered)} tone="ok" />
          <StatusTile label={t('已启用 MCP', 'Enabled MCP')} value={String(summary.enabledMCP)} tone="ok" />
          <StatusTile label={t('Cloud 来源', 'Cloud source')} value={String(summary.cloudItems)} />
        </div>
        <div className="cloud-actions">
          <label className="capability-search-field">
            <span>{t('搜索', 'Search')}</span>
            <input
              value={packageQuery}
              onChange={event => setPackageQuery(event.target.value)}
              placeholder={t('能力名、来源、iWorker', 'Package, source, iWorker')}
            />
          </label>
          <button className="ghost" type="button" onClick={() => { void loadCaps(); }} disabled={loading}>
            {loading ? t('刷新中...', 'Refreshing...') : t('刷新能力包', 'Refresh packages')}
          </button>
          <span className="cloud-inline-note">{t('可下发 iWorker：', 'Deliverable iWorkers: ')}{colleagues.length} / {t('高风险：', 'High risk: ')}{summary.highRisk}</span>
        </div>
        {message ? <p className={`cloud-message ${message.kind}`}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard
        title={t('从 Cloud 市场导入 Skill', 'Import Skill from Cloud Market')}
        desc={t(
          'Center 只拉取授权范围内的 Skill 包并落入本地审核队列；后续安装、绑定和下发仍由本 Center 管理。',
          'Center only pulls licensed Skill packages into the local review queue. Installation, binding, and delivery remain managed by this Center.',
        )}
      >
        <div className="cloud-actions">
          <label className="capability-search-field">
            <span>{t('Cloud Skill', 'Cloud Skill')}</span>
            <input
              value={cloudQuery}
              onChange={event => setCloudQuery(event.target.value)}
              onKeyDown={event => {
                if (event.key === 'Enter') void searchCloudMarket();
              }}
              placeholder={t('搜索名称、分类或标签', 'Search name, category, or tag')}
            />
          </label>
          <button className="primary" type="button" onClick={() => { void searchCloudMarket(); }} disabled={cloudSearching}>
            {cloudSearching ? t('搜索中...', 'Searching...') : t('搜索 Cloud 市场', 'Search Cloud Market')}
          </button>
          <span className="cloud-inline-note">{t('导入后会进入待审核，不会直接下发给 iWorker。', 'Imported Skills enter pending review and are not delivered directly to iWorkers.')}</span>
        </div>
        <div className="cloud-market-grid capability-market-grid">
          {cloudResults.map(skill => {
            const alreadyImported = caps.some(cap => cap.source === `iworkercloud:${skill.id}` || cap.source === `cloud:${skill.id}`);
            return (
              <article key={skill.id} className="cloud-pillar-card card">
                <div className="item-head">
                  <div>
                    <span className="mini">{skill.category || t('通用', 'General')} / {skill.version || '1.0.0'}</span>
                    <h3>{skill.name || skill.id}</h3>
                  </div>
                  <span className={`badge ${toneForRisk(skill.risk_level || 'low')}`}>{riskLabel(skill.risk_level || 'low')}</span>
                </div>
                <strong>{skill.id}</strong>
                <p>{skill.description || t('暂无描述', 'No description yet.')}</p>
                <div className="cloud-pill-list">
                  {(skill.tags || []).slice(0, 6).map(tag => <span key={tag}>{tag}</span>)}
                  <span>{t('下载', 'Downloads')}: {skill.download_count || skill.downloads || 0}</span>
                  {skill.package_sha256 ? <span>sha256: {skill.package_sha256.slice(0, 12)}...</span> : <span>{t('包待确认', 'Package pending')}</span>}
                </div>
                <div className="actions">
                  <button
                    className={alreadyImported ? 'btn-ghost' : 'btn-secondary'}
                    type="button"
                    disabled={alreadyImported || importingSkillID === skill.id}
                    onClick={() => { void importCloudSkill(skill); }}
                  >
                    {alreadyImported ? t('已导入', 'Imported') : importingSkillID === skill.id ? t('导入中...', 'Importing...') : t('导入到审核队列', 'Import to review')}
                  </button>
                </div>
              </article>
            );
          })}
          {cloudResults.length === 0 ? <p className="cloud-inline-note">{t('搜索 Cloud 市场后，可在这里选择 Skill 导入。', 'Search Cloud Market, then import selected Skills here.')}</p> : null}
        </div>
      </SectionCard>

      {pendingCaps.length > 0 && (
        <SectionCard
          title={t('待审核', 'Pending Review')}
          desc={t('来自 Cloud 市场或外部来源的能力包，需要先审核再安装和下发。', 'Packages from Cloud Market or external sources must be reviewed before installation and delivery.')}
        >
        <CapabilityTable caps={pendingCaps} colleagues={colleagues} bindingsByCapability={bindingsByCapability} selectedWorker={selectedWorker} setSelectedWorker={setSelectedWorker} busy={busy} onApprove={cap => runAction(cap, 'approve')} onReject={cap => runAction(cap, 'reject')} onInstall={cap => runAction(cap, 'install')} onBind={bindToWorker} onUnbind={unbindFromWorker} labels={{ statusLabel, riskLabel, sourceLabel, packageStatusLabel }} />
        </SectionCard>
      )}

      <SectionCard
        title={t('按 iWorker 查看下发', 'Delivery by iWorker')}
        desc={t(
          '按数字同事聚合查看已下发 Skill，管理员可以先确认每个 iWorker 的能力边界，再进入能力包表格执行下发或撤回。',
          'Review delivered Skills by digital coworker first, then use the package tables below to deliver or revoke capabilities.',
        )}
      >
        <div className="capability-worker-grid">
          {workerDeliverySummary.map(item => (
            <article key={item.worker.id} className={item.delivered.length ? 'capability-worker-card' : 'capability-worker-card is-empty'}>
              <div className="capability-worker-head">
                <div>
                  <strong>{item.worker.name || item.worker.id}</strong>
                  <span>{item.worker.role_name || item.worker.role_code || item.worker.status || t('未设置角色', 'No role')}</span>
                </div>
                <em>{item.delivered.length}</em>
              </div>
              <div className="capability-worker-tags">
                {item.delivered.length > 0 ? item.delivered.slice(0, 6).map(binding => (
                  <span key={binding.id}>{binding.capability_name || binding.capability_id}</span>
                )) : <span className="muted">{t('尚未下发 Skill', 'No Skill delivered yet')}</span>}
              </div>
              {item.delivered.length > 6 ? <p className="cloud-inline-note">{t('另有 ', 'Plus ')}{item.delivered.length - 6}{t(' 个能力未展开', ' more capabilities')}</p> : null}
            </article>
          ))}
          {workerDeliverySummary.length === 0 ? (
            <div className="empty-state">
              <strong>{t('还没有 iWorker', 'No iWorker yet')}</strong>
              <p>{t('请先在组织或 Bootstrap 中创建数字同事，然后再下发 Skill/MCP。', 'Create digital coworkers in organization setup or bootstrap before delivering Skill/MCP.')}</p>
            </div>
          ) : null}
        </div>
      </SectionCard>

      <MCPSection
        servers={filteredMCPServers}
        draft={mcpDraft}
        setDraft={setMCPDraft}
        busy={busy}
        query={packageQuery}
        riskLabel={riskLabel}
        onCreate={createMCPServer}
        onSetStatus={setMCPStatus}
      />

      <SectionCard
        title={t('已安装与可用能力包', 'Installed and Available Packages')}
        desc={t(
          '共 ' + readyCaps.length + ' 个能力包。先安装运行时入口，再绑定到对应 iWorker。',
          'Total ' + readyCaps.length + ' packages. Install runtime entry first, then bind the package to an iWorker.',
        )}
      >
        <CapabilityTable caps={readyCaps} colleagues={colleagues} bindingsByCapability={bindingsByCapability} selectedWorker={selectedWorker} setSelectedWorker={setSelectedWorker} busy={busy} onApprove={cap => runAction(cap, 'approve')} onReject={cap => runAction(cap, 'reject')} onInstall={cap => runAction(cap, 'install')} onBind={bindToWorker} onUnbind={unbindFromWorker} labels={{ statusLabel, riskLabel, sourceLabel, packageStatusLabel }} />
        {readyCaps.length === 0 && <p className="cloud-inline-note">{packageQuery.trim() ? t('没有匹配的能力包。', 'No matching capability packages.') : t('还没有启用的能力包。可以从 Cloud 市场导入，或在 Center 本地安装 MCP/Skill。', 'No enabled packages yet. Import from Cloud Market or install local MCP/Skill packages in Center.')}</p>}
      </SectionCard>
    </div>
  );
}

type CapabilityTableProps = {
  caps: Capability[];
  colleagues: Colleague[];
  bindingsByCapability: Map<string, CapabilityBinding[]>;
  selectedWorker: Record<string, string>;
  setSelectedWorker: (value: Record<string, string>) => void;
  busy: string;
  onApprove: (cap: Capability) => void;
  onReject: (cap: Capability) => void;
  onInstall: (cap: Capability) => void;
  onBind: (cap: Capability) => void;
  onUnbind: (cap: Capability, binding: CapabilityBinding) => void;
  labels: { statusLabel: (v: string) => string; riskLabel: (v: string) => string; sourceLabel: (v?: string) => string; packageStatusLabel: (v?: string) => string };
};

function CapabilityTable({ caps, colleagues, bindingsByCapability, selectedWorker, setSelectedWorker, busy, onApprove, onReject, onInstall, onBind, onUnbind, labels }: CapabilityTableProps) {
  const { t } = useI18n();
  return (
    <div className="data-table-wrap capability-action-table">
      <table className="data-table">
        <thead>
          <tr>
            <th>{t('名称', 'Name')}</th>
            <th>{t('来源', 'Source')}</th>
            <th>{t('版本', 'Version')}</th>
            <th>{t('风险', 'Risk')}</th>
            <th>{t('状态', 'Status')}</th>
            <th>{t('安装', 'Install')}</th>
            <th>{t('下发到 iWorker', 'Deliver to iWorker')}</th>
            <th>{t('操作', 'Actions')}</th>
          </tr>
        </thead>
        <tbody>
          {caps.map(cap => {
            const isBusy = busy.startsWith(`${cap.id}:`);
            const deliveredBindings = bindingsByCapability.get(cap.id) || [];
            const selectedColleagueID = selectedWorker[cap.id] || colleagues[0]?.id || '';
            const alreadyDeliveredToSelected = deliveredBindings.some(item => item.colleague_id === selectedColleagueID);
            return (
              <tr key={cap.id}>
                <td><strong>{cap.name}</strong><br /><small>{cap.description || cap.category || '-'}</small></td>
                <td>{labels.sourceLabel(cap.source)}</td>
                <td>{cap.version || '-'}</td>
                <td><span className={`badge ${toneForRisk(cap.risk_level)}`}>{labels.riskLabel(cap.risk_level)}</span></td>
                <td>{labels.statusLabel(cap.status)}</td>
                <td>{labels.packageStatusLabel(cap.package_status)}</td>
                <td>
                  <div className="capability-bind-controls">
                    <select value={selectedWorker[cap.id] || colleagues[0]?.id || ''} onChange={e => setSelectedWorker({ ...selectedWorker, [cap.id]: e.target.value })} disabled={!colleagues.length || isBusy}>
                      {colleagues.length === 0 ? <option value="">{t('无 iWorker', 'No iWorker')}</option> : colleagues.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}
                    </select>
                    <button
                      className="btn-secondary"
                      type="button"
                      title={!isRuntimeInstalled(cap) ? t('请先安装运行时入口，再下发给 iWorker。', 'Install the runtime entry before delivering to iWorker.') : undefined}
                      disabled={!canDeliver(cap) || !colleagues.length || isBusy || alreadyDeliveredToSelected}
                      onClick={() => onBind(cap)}
                    >
                      {alreadyDeliveredToSelected ? t('已下发', 'Delivered') : busy === `${cap.id}:bind` ? t('下发中...', 'Delivering...') : t('下发', 'Deliver')}
                    </button>
                  </div>
                  <div className="capability-delivery-list">
                    {deliveredBindings.length > 0 ? deliveredBindings.map(binding => (
                      <span key={binding.id}>
                        {binding.colleague_name || binding.colleague_id}
                        <button
                          type="button"
                          aria-label={t('撤回下发', 'Revoke delivery') + ' ' + (binding.colleague_name || binding.colleague_id)}
                          disabled={busy === `${cap.id}:unbind:${binding.colleague_id}`}
                          onClick={() => onUnbind(cap, binding)}
                        >
                          {busy === `${cap.id}:unbind:${binding.colleague_id}` ? t('撤回中', 'Revoking') : t('撤回', 'Revoke')}
                        </button>
                      </span>
                    )) : <span className="muted">{t('尚未下发', 'Not delivered')}</span>}
                  </div>
                </td>
                <td>
                  <div className="capability-row-actions">
                    {canApprove(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onApprove(cap)}>{busy === `${cap.id}:approve` ? t('批准中...', 'Approving...') : t('批准', 'Approve')}</button>}
                    {canReject(cap) && <button className="btn-secondary" type="button" disabled={isBusy} onClick={() => onReject(cap)}>{t('拒绝', 'Reject')}</button>}
                    {(cap.status === 'active' || cap.status === 'approved') && !isRuntimeInstalled(cap) ? (
                      <button
                        className="btn-secondary"
                        type="button"
                        title={!canInstall(cap) ? t('需要先缓存可下载包，才能安装运行时入口。', 'A downloadable package must be cached before installing the runtime entry.') : undefined}
                        disabled={isBusy || !canInstall(cap)}
                        onClick={() => onInstall(cap)}
                      >
                        {busy === `${cap.id}:install` ? t('安装中...', 'Installing...') : t('安装', 'Install')}
                      </button>
                    ) : null}
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

type MCPSectionProps = {
  servers: MCPServer[];
  draft: MCPDraft;
  setDraft: (value: MCPDraft) => void;
  busy: string;
  query: string;
  riskLabel: (value: string) => string;
  onCreate: () => void;
  onSetStatus: (server: MCPServer, status: 'enabled' | 'disabled') => void;
};

function MCPSection({ servers, draft, setDraft, busy, query, riskLabel, onCreate, onSetStatus }: MCPSectionProps) {
  const { t } = useI18n();
  return (
    <SectionCard
      title={t('企业 MCP 服务', 'Enterprise MCP Services')}
      desc={t(
        '在 Center 本地安装企业需要的 MCP，并按部门范围下发给 iWorker。这里只保存环境变量名，不保存密钥值，密钥由部署环境提供。',
        'Install enterprise MCP services locally in Center and deliver them to iWorkers by department scope. Only environment variable names are stored here; secret values stay in the deployment environment.',
      )}
    >
      <div className="cloud-form-grid">
        <label><span>{t('名称', 'Name')}</span><input value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} placeholder={t('例如：CRM MCP', 'e.g. CRM MCP')} /></label>
        <label><span>{t('传输类型', 'Transport')}</span><select value={draft.server_type} onChange={e => setDraft({ ...draft, server_type: e.target.value as MCPDraft['server_type'] })}><option value="http">HTTP</option><option value="sse">SSE</option><option value="stdio">STDIO</option></select></label>
        <label><span>{draft.server_type === 'stdio' ? t('命令', 'Command') : t('服务端点', 'Endpoint')}</span><input value={draft.server_type === 'stdio' ? draft.command : draft.endpoint} onChange={e => setDraft(draft.server_type === 'stdio' ? { ...draft, command: e.target.value } : { ...draft, endpoint: e.target.value })} placeholder={draft.server_type === 'stdio' ? 'mcp-server-command' : 'https://mcp.example.com'} /></label>
        <label><span>{t('部门范围', 'Department scope')}</span><input value={draft.department_id} onChange={e => setDraft({ ...draft, department_id: e.target.value })} placeholder="all / finance / ops" /></label>
        <label><span>{t('风险等级', 'Risk level')}</span><select value={draft.risk_level} onChange={e => setDraft({ ...draft, risk_level: e.target.value })}><option value="low">{t('低', 'Low')}</option><option value="medium">{t('中', 'Medium')}</option><option value="high">{t('高', 'High')}</option></select></label>
        <label><span>{t('参数', 'Arguments')}</span><input value={draft.args} onChange={e => setDraft({ ...draft, args: e.target.value })} placeholder={t('逗号或换行分隔', 'Comma or newline separated')} /></label>
        <label><span>{t('环境变量名', 'Environment keys')}</span><input value={draft.env_keys} onChange={e => setDraft({ ...draft, env_keys: e.target.value })} placeholder="CRM_TOKEN, CRM_BASE_URL" /></label>
        <label><span>{t('描述', 'Description')}</span><input value={draft.description} onChange={e => setDraft({ ...draft, description: e.target.value })} placeholder={t('用途、数据边界、适用部门', 'Purpose, data boundary, target departments')} /></label>
      </div>
      <div className="cloud-actions">
        <button className="primary" type="button" onClick={() => onCreate()} disabled={busy === 'mcp:create'}>{busy === 'mcp:create' ? t('安装中...', 'Installing...') : t('安装并启用 MCP', 'Install and enable MCP')}</button>
        <span className="cloud-inline-note">{t('iWorker 客户端只会看到 enabled 且匹配部门范围的 MCP。', 'iWorker clients only see enabled MCP services matching their department scope.')}</span>
      </div>
      <div className="data-table-wrap capability-action-table">
        <table className="data-table">
          <thead><tr><th>{t('名称', 'Name')}</th><th>{t('类型', 'Type')}</th><th>{t('范围', 'Scope')}</th><th>{t('风险', 'Risk')}</th><th>{t('环境变量', 'Env keys')}</th><th>{t('状态', 'Status')}</th><th>{t('操作', 'Actions')}</th></tr></thead>
          <tbody>
            {servers.map(server => (
              <tr key={server.id}>
                <td><strong>{server.name}</strong><br /><small>{server.description || server.endpoint || server.command || server.id}</small></td>
                <td>{server.server_type}</td>
                <td>{server.department_id || 'all'}</td>
                <td><span className={`badge ${toneForRisk(server.risk_level)}`}>{riskLabel(server.risk_level)}</span></td>
                <td>{(server.env_keys || []).join(', ') || '-'}</td>
                <td>{server.status === 'enabled' ? t('启用', 'Enabled') : t('停用', 'Disabled')}</td>
                <td><div className="capability-row-actions">{server.status === 'enabled' ? <button className="btn-secondary" type="button" disabled={busy === `mcp:${server.id}:disabled`} onClick={() => onSetStatus(server, 'disabled')}>{t('停用', 'Disable')}</button> : <button className="btn-secondary" type="button" disabled={busy === `mcp:${server.id}:enabled`} onClick={() => onSetStatus(server, 'enabled')}>{t('启用', 'Enable')}</button>}</div></td>
              </tr>
            ))}
          </tbody>
        </table>
        {servers.length === 0 ? <p className="cloud-inline-note">{query.trim() ? t('没有匹配的 MCP 服务。', 'No matching MCP services.') : t('还没有安装企业 MCP。', 'No enterprise MCP service installed yet.')}</p> : null}
      </div>
    </SectionCard>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={`cloud-status-tile ${tone || ''}`}><span>{label}</span><strong>{value}</strong></div>;
}
