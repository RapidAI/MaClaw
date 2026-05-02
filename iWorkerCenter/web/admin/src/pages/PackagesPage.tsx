import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { InstitutionalizationStageRail, type InstitutionalizationStageCard } from '../components/assets/InstitutionalizationStageRail';
import { approveCapability, createMCPServer, listCapabilities, listMCPServers, updateMCPServer, type Capability, type MCPServer, type MCPServerInput } from '../api/capabilities';
import type { AssetNavigationTarget, CenterTab, OverviewNavigationTarget } from '../types';

type PackagesPageProps = {
  navigationTarget?: AssetNavigationTarget | null;
  onNavigationHandled?: () => void;
  onNavigateToOverview?: (target?: OverviewNavigationTarget | null) => void;
  onNavigateToTab: (tab: CenterTab, target?: AssetNavigationTarget) => void;
};

export function PackagesPage({ navigationTarget, onNavigationHandled, onNavigateToOverview, onNavigateToTab }: PackagesPageProps) {
  const { t } = useTranslation();
  const [items, setItems] = useState<Capability[]>([]);
  const [mcpServers, setMCPServers] = useState<MCPServer[]>([]);
  const [mcpLoading, setMCPLoading] = useState(true);
  const [mcpSaving, setMCPSaving] = useState(false);
  const [mcpUpdatingId, setMCPUpdatingId] = useState('');
  const [mcpMessage, setMCPMessage] = useState('');
  const [mcpDraft, setMCPDraft] = useState({
    name: '',
    description: '',
    serverType: 'http' as MCPServerInput['server_type'],
    endpoint: '',
    command: '',
    args: '',
    envKeys: '',
    departmentId: 'all',
    riskLevel: 'medium',
    status: 'enabled' as MCPServerInput['status'],
  });
  const [loading, setLoading] = useState(true);
  const [focusedTarget, setFocusedTarget] = useState<AssetNavigationTarget | null>(null);
  const [message, setMessage] = useState('');
  const [approving, setApproving] = useState(false);

  const loadItems = async () => {
    setLoading(true);
    try {
      const nextItems = await listCapabilities();
      setItems(nextItems);
    } catch {
    } finally {
      setLoading(false);
    }
  };


  const loadMCPServers = async () => {
    setMCPLoading(true);
    try {
      const nextServers = await listMCPServers();
      setMCPServers(nextServers);
    } catch (error) {
      setMCPMessage(error instanceof Error ? error.message : 'Failed to load MCP servers.');
    } finally {
      setMCPLoading(false);
    }
  };

  useEffect(() => {
    void loadItems();
    void loadMCPServers();
  }, []);

  useEffect(() => {
    if (!navigationTarget) {
      return;
    }
    setFocusedTarget(navigationTarget);
    onNavigationHandled?.();
  }, [navigationTarget, onNavigationHandled]);

  const filteredItems = useMemo(() => {
    if (!focusedTarget) {
      return items;
    }
    return items.filter((item) => {
      if (focusedTarget.draft_id && item.id === focusedTarget.draft_id) {
        return true;
      }
      if (focusedTarget.draft_name && item.name === focusedTarget.draft_name) {
        return true;
      }
      if (focusedTarget.role_label && item.name.toLowerCase().includes(focusedTarget.role_label.toLowerCase())) {
        return true;
      }
      if (focusedTarget.role_code && item.name.toLowerCase().includes(focusedTarget.role_code.toLowerCase())) {
        return true;
      }
      return false;
    });
  }, [items, focusedTarget]);

  const primaryItem = filteredItems[0] || null;
  const canApprove = Boolean(primaryItem && primaryItem.status !== 'approved');
  const roleLabel = focusedTarget?.role_label || focusedTarget?.role_code || 'Recovery deposition';
  const stageTarget = focusedTarget || undefined;

  const stageCards = useMemo<InstitutionalizationStageCard[]>(() => [
    {
      id: 'knowledge',
      eyebrow: 'Stage 1',
      title: 'Review recovery memory',
      tone: 'ok',
      summary: 'Make sure the recovery path is described clearly before promoting it into a reusable capability.',
      detail: 'The memory stage preserves reasoning and language. It is the source material for the capability package.',
      statLine: [
        'Memory first',
        'Clarify role context',
        'Preserve exception language',
      ],
      actionLabel: 'Open knowledge',
    },
    {
      id: 'packages',
      eyebrow: 'Stage 2',
      title: primaryItem ? `${primaryItem.name} is under package review` : 'Approve the capability package',
      tone: primaryItem ? (primaryItem.status === 'approved' ? 'ok' : 'info') : 'warn',
      summary: primaryItem
        ? `${primaryItem.name} currently sits in status ${primaryItem.status}.`
        : 'No matching capability package is currently visible for this recovery context.',
      detail: 'This is where the organization decides whether the recovery logic is mature enough to become a reusable packaged capability.',
      statLine: [
        `${filteredItems.length} matching capability package${filteredItems.length === 1 ? '' : 's'}`,
        `${items.length} total package${items.length === 1 ? '' : 's'}`,
        primaryItem?.status || 'awaiting review',
      ],
      actionLabel: 'Current stage',
    },
    {
      id: 'workflows',
      eyebrow: 'Stage 3',
      title: 'Publish workflow standard',
      tone: primaryItem?.status === 'approved' ? 'info' : 'ok',
      summary: 'Once the package is approved, the next move is to publish the workflow so the center can execute it under policy.',
      detail: 'A capability package is a reviewed asset. A workflow publication is what turns that asset into active organizational behavior.',
      statLine: [
        'Package approval before rollout',
        'Policy follows package review',
        'Shift into monitoring mode',
      ],
      actionLabel: 'Open workflows',
    },
  ], [filteredItems.length, items.length, primaryItem]);

  const rows = filteredItems.map((c) => ({
    name: c.name,
    description: c.description,
    status: c.status,
  }));


  const mcpRows = mcpServers.map((server) => ({
    name: server.name,
    type: server.server_type,
    department: server.department_id || 'all',
    status: server.status,
    target: server.server_type === 'stdio' ? (server.command || '-') : (server.endpoint || '-'),
  }));

  const handleApprove = async () => {
    if (!primaryItem) {
      return;
    }
    try {
      setApproving(true);
      setMessage('');
      await approveCapability(primaryItem.id);
      await loadItems();
      setMessage(`Approved capability package ${primaryItem.name}. The organization can now use this recovery handling package as a formal reusable asset.`);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Failed to approve the capability package.');
    } finally {
      setApproving(false);
    }
  };


  const parseList = (value: string) => value.split(/[\n,]/).map((item) => item.trim()).filter(Boolean);

  const mcpServerToInput = (server: MCPServer, status: MCPServerInput['status']): MCPServerInput => ({
    name: server.name,
    description: server.description,
    server_type: server.server_type === 'sse' || server.server_type === 'stdio' ? server.server_type : 'http',
    endpoint: server.endpoint,
    command: server.command || '',
    args: server.args || [],
    env_keys: server.env_keys || [],
    department_id: server.department_id || 'all',
    risk_level: server.risk_level || 'medium',
    status,
  });

  const handleToggleMCPServer = async (server: MCPServer) => {
    const nextStatus: MCPServerInput['status'] = server.status === 'enabled' ? 'disabled' : 'enabled';
    try {
      setMCPUpdatingId(server.id);
      setMCPMessage('');
      await updateMCPServer(server.id, mcpServerToInput(server, nextStatus));
      await loadMCPServers();
      setMCPMessage(`MCP server ${server.name} is now ${nextStatus}. Scoped iWorkers will reflect this on next refresh.`);
    } catch (error) {
      setMCPMessage(error instanceof Error ? error.message : 'Failed to update MCP server.');
    } finally {
      setMCPUpdatingId('');
    }
  };

  const handleCreateMCPServer = async () => {
    if (!mcpDraft.name.trim()) {
      setMCPMessage('MCP name is required.');
      return;
    }
    if (mcpDraft.serverType === 'stdio' && !mcpDraft.command.trim()) {
      setMCPMessage('Command is required for stdio MCP servers.');
      return;
    }
    if (mcpDraft.serverType !== 'stdio' && !mcpDraft.endpoint.trim()) {
      setMCPMessage('Endpoint is required for network MCP servers.');
      return;
    }
    try {
      setMCPSaving(true);
      setMCPMessage('');
      await createMCPServer({
        name: mcpDraft.name.trim(),
        description: mcpDraft.description.trim(),
        server_type: mcpDraft.serverType,
        endpoint: mcpDraft.endpoint.trim(),
        command: mcpDraft.command.trim(),
        args: parseList(mcpDraft.args),
        env_keys: parseList(mcpDraft.envKeys),
        department_id: mcpDraft.departmentId.trim() || 'all',
        risk_level: mcpDraft.riskLevel.trim() || 'medium',
        status: mcpDraft.status || 'enabled',
      });
      await loadMCPServers();
      setMCPDraft((current) => ({ ...current, name: '', description: '', endpoint: '', command: '', args: '', envKeys: '' }));
      setMCPMessage('MCP server installed in iWorkerCenter and will be visible to scoped iWorkers on their next refresh.');
    } catch (error) {
      setMCPMessage(error instanceof Error ? error.message : 'Failed to install MCP server.');
    } finally {
      setMCPSaving(false);
    }
  };

  return (
    <div className="center-page-stack">
      <InstitutionalizationStageRail
        activeStage="packages"
        cards={stageCards}
        navigationTarget={stageTarget}
        overviewTarget={{ role_code: focusedTarget?.role_code, source: 'package_review' }}
        onNavigateToOverview={onNavigateToOverview}
        onNavigateToTab={onNavigateToTab}
      />

      <SectionCard title="Center-managed MCP" desc={mcpLoading ? t('common.loading') : `${mcpRows.length} server${mcpRows.length === 1 ? '' : 's'}`}>
        <div className="mcp-install-grid">
          <div className="mcp-install-copy">
            <span className="badge info">Center installed</span>
            <strong>MCP servers are installed here, then distributed to iWorkers by department scope.</strong>
            <p>iWorkerCloud may provide entitlement or package source later, but business MCP selection and runtime visibility stay inside iWorkerCenter. Secrets are represented as env key names only; token values stay in the runtime environment.</p>
          </div>
          <div className="mcp-install-form">
            <input value={mcpDraft.name} onChange={(event) => setMCPDraft((current) => ({ ...current, name: event.target.value }))} placeholder="MCP name" />
            <select value={mcpDraft.serverType} onChange={(event) => setMCPDraft((current) => ({ ...current, serverType: event.target.value as MCPServerInput['server_type'] }))}>
              <option value="http">HTTP</option>
              <option value="sse">SSE</option>
              <option value="stdio">stdio</option>
            </select>
            <input value={mcpDraft.departmentId} onChange={(event) => setMCPDraft((current) => ({ ...current, departmentId: event.target.value }))} placeholder="Department scope, e.g. finance or all" />
            <select value={mcpDraft.status} onChange={(event) => setMCPDraft((current) => ({ ...current, status: event.target.value as MCPServerInput['status'] }))}>
              <option value="enabled">Enabled</option>
              <option value="disabled">Disabled</option>
            </select>
            <input value={mcpDraft.endpoint} onChange={(event) => setMCPDraft((current) => ({ ...current, endpoint: event.target.value }))} placeholder="Endpoint for HTTP/SSE" />
            <input value={mcpDraft.command} onChange={(event) => setMCPDraft((current) => ({ ...current, command: event.target.value }))} placeholder="Command for stdio" />
            <input value={mcpDraft.args} onChange={(event) => setMCPDraft((current) => ({ ...current, args: event.target.value }))} placeholder="Args, comma separated" />
            <input value={mcpDraft.envKeys} onChange={(event) => setMCPDraft((current) => ({ ...current, envKeys: event.target.value }))} placeholder="Env variable names only, e.g. CRM_TOKEN" />
            <input value={mcpDraft.riskLevel} onChange={(event) => setMCPDraft((current) => ({ ...current, riskLevel: event.target.value }))} placeholder="Risk level" />
            <textarea value={mcpDraft.description} onChange={(event) => setMCPDraft((current) => ({ ...current, description: event.target.value }))} placeholder="What this MCP lets iWorkers do" rows={3} />
            <button type="button" className="executive-assign-button" disabled={mcpSaving} onClick={() => void handleCreateMCPServer()}>
              {mcpSaving ? 'Installing...' : 'Install MCP'}
            </button>
          </div>
        </div>
        {mcpMessage ? <p className="mcp-install-message">{mcpMessage}</p> : null}
        <DataTable
          columns={[
            { key: 'name', label: 'Name' },
            { key: 'type', label: 'Type' },
            { key: 'department', label: 'Department' },
            { key: 'target', label: 'Runtime target' },
            { key: 'status', label: 'Status' },
          ]}
          rows={mcpRows}
        />
        {mcpServers.length > 0 ? (
          <div className="mcp-server-action-list">
            {mcpServers.map((server) => (
              <article key={server.id} className="mcp-server-action-card">
                <div>
                  <strong>{server.name}</strong>
                  <span>{server.server_type} / {server.department_id || 'all'} / {server.status}</span>
                </div>
                <button type="button" className="executive-link-button" disabled={mcpUpdatingId === server.id} onClick={() => void handleToggleMCPServer(server)}>
                  {mcpUpdatingId === server.id ? 'Updating...' : server.status === 'enabled' ? 'Disable' : 'Enable'}
                </button>
              </article>
            ))}
          </div>
        ) : null}
      </SectionCard>

      <SectionCard title={t('nav.packages')} desc={loading ? t('common.loading') : `${rows.length}`}>
        {focusedTarget ? (
          <div className="item-row asset-review-row">
            <span className="badge info">Capability package review</span>
            <strong>{roleLabel}</strong>
            <p>{focusedTarget.draft_name ? `Showing the capability package expected for ${focusedTarget.draft_name}.` : 'Showing capability packages related to the current recovery deposition context.'}</p>
            {primaryItem ? <p>{`Primary package status: ${primaryItem.status}`}</p> : <p>No matching package draft is currently visible for this recovery context.</p>}
            <p>This is the approval gate where the organization decides the recovery logic is reusable enough to be promoted into a formal operating package.</p>
            <div className="executive-action-row">
              <button
                type="button"
                className="executive-assign-button"
                disabled={!canApprove || approving}
                onClick={() => void handleApprove()}
              >
                {approving ? 'Approving...' : primaryItem?.status === 'approved' ? 'Package approved' : 'Approve package'}
              </button>
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToTab('knowledge', stageTarget)}
              >
                Review knowledge again
              </button>
              <button
                type="button"
                className="executive-link-button"
                onClick={() => onNavigateToTab('workflows', stageTarget)}
              >
                Continue to workflows
              </button>
            </div>
            {message ? <p>{message}</p> : null}
          </div>
        ) : null}
        <DataTable
          columns={[
            { key: 'name', label: 'Name' },
            { key: 'description', label: 'Description' },
            { key: 'status', label: 'Status' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}
