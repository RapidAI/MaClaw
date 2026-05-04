import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  CenterEnrollmentDiscovery,
  CenterHealthStatus,
  DiWorkerSettings,
  UpstreamProvider,
  WorkerMemoryEntry,
  WorkerMemoryStats,
} from "../types";

type Props = {
  settings: DiWorkerSettings;
  loading: boolean;
  saving: boolean;
  dirty: boolean;
  error: string;
  saveMessage: string;
  healthChecking: boolean;
  healthStatus: CenterHealthStatus | null;
  healthError: string;
  enrollmentDiscovery: CenterEnrollmentDiscovery | null;
  enrollmentDiscovering: boolean;
  enrollmentApplyingId: string;
  enrollmentMessage: string;
  enrollmentError: string;
  memoryStats: WorkerMemoryStats | null;
  memoryStatsLoading: boolean;
  memoryStatsError: string;
  memoryDraftScope: string;
  memoryDraftContent: string;
  memoryDraftCategory: string;
  memoryDraftTags: string;
  memorySaving: boolean;
  memorySaveMessage: string;
  memorySaveError: string;
  memoryRecallQuery: string;
  memoryRecallItems: WorkerMemoryEntry[];
  memoryRecallLoading: boolean;
  memoryRecallError: string;
  memoryDeletingId: string;
  memoryDeleteError: string;
  onRoleNameChange: (value: string) => void;
  onRoleDescriptionChange: (value: string) => void;
  onCenterEnabledChange: (value: boolean) => void;
  onCenterHostChange: (value: string) => void;
  onCenterPortChange: (value: string) => void;
  onCenterBaseUrlChange: (value: string) => void;
  onCenterTenantIdChange: (value: string) => void;
  onCenterDepartmentIdChange: (value: string) => void;
  onCenterWorkerIdChange: (value: string) => void;
  onCenterTimeoutChange: (value: string) => void;
  onGoalWatchAutoHandleEnabledChange: (value: boolean) => void;
  onGoalWatchIntervalChange: (value: string) => void;
  onGoalWatchMaxDurationChange: (value: string) => void;
  onRoutingModeChange: (value: DiWorkerSettings["routing"]["mode"]) => void;
  onRoutingDefaultProviderChange: (value: string) => void;
  onRoutingAllowFallbackChange: (value: boolean) => void;
  onProviderChange: (
    providerId: string,
    patch: Partial<UpstreamProvider>,
  ) => void;
  onProviderFeaturesChange: (providerId: string, value: string) => void;
  onCheckCenterHealth: () => void;
  onDiscoverCenterEnrollment: () => void;
  onApplyCenterEnrollment: (
    workerId: string,
    auth: { method: string; username: string; password: string },
  ) => void;
  onRefreshMemoryStats: () => void;
  onMemoryDraftScopeChange: (value: string) => void;
  onMemoryDraftContentChange: (value: string) => void;
  onMemoryDraftCategoryChange: (value: string) => void;
  onMemoryDraftTagsChange: (value: string) => void;
  onSaveWorkerMemory: () => void;
  onMemoryRecallQueryChange: (value: string) => void;
  onRecallWorkerMemories: () => void;
  onDeleteWorkerMemory: (memoryId: string) => void;
  onSave: () => void;
};

const SectionIcon = ({ children }: { children: string }) => (
  <span className="dw-inline-icon" aria-hidden="true">
    {children}
  </span>
);

const defaultAuthMethods = [
  {
    method: "local",
    label: "localAccount",
    enabled: true,
    implemented: true,
    status: "ready",
    description: "localAccountDesc",
  },
  {
    method: "ldap",
    label: "LDAP",
    enabled: true,
    implemented: true,
    status: "available",
    description: "ldapDesc",
  },
  {
    method: "oidc",
    label: "oidcSso",
    enabled: false,
    implemented: false,
    status: "reserved",
    description: "oidcDesc",
  },
];

const statusText = (
  enabled: boolean,
  enabledText: string,
  disabledText: string,
) => (enabled ? enabledText : disabledText);

export function SettingsPage({
  settings,
  loading,
  saving,
  dirty,
  error,
  saveMessage,
  healthChecking,
  healthStatus,
  healthError,
  enrollmentDiscovery,
  enrollmentDiscovering,
  enrollmentApplyingId,
  enrollmentMessage,
  enrollmentError,
  memoryStats,
  memoryStatsLoading,
  memoryStatsError,
  memoryDraftScope,
  memoryDraftContent,
  memoryDraftCategory,
  memoryDraftTags,
  memorySaving,
  memorySaveMessage,
  memorySaveError,
  memoryRecallQuery,
  memoryRecallItems,
  memoryRecallLoading,
  memoryRecallError,
  memoryDeletingId,
  memoryDeleteError,
  onRoleNameChange,
  onRoleDescriptionChange,
  onCenterEnabledChange,
  onCenterHostChange,
  onCenterPortChange,
  onCenterBaseUrlChange,
  onCenterTenantIdChange,
  onCenterDepartmentIdChange,
  onCenterWorkerIdChange,
  onCenterTimeoutChange,
  onGoalWatchAutoHandleEnabledChange,
  onGoalWatchIntervalChange,
  onGoalWatchMaxDurationChange,
  onRoutingModeChange,
  onRoutingDefaultProviderChange,
  onRoutingAllowFallbackChange,
  onProviderChange,
  onProviderFeaturesChange,
  onCheckCenterHealth,
  onDiscoverCenterEnrollment,
  onApplyCenterEnrollment,
  onRefreshMemoryStats,
  onMemoryDraftScopeChange,
  onMemoryDraftContentChange,
  onMemoryDraftCategoryChange,
  onMemoryDraftTagsChange,
  onSaveWorkerMemory,
  onMemoryRecallQueryChange,
  onRecallWorkerMemories,
  onDeleteWorkerMemory,
  onSave,
}: Props) {
  const { t } = useTranslation();
  const st = (
    key: string,
    fallback: string,
    options?: Record<string, unknown>,
  ) => t(`settings.${key}`, { defaultValue: fallback, ...options });
  const [expandedProviderId, setExpandedProviderId] = useState<string | null>(
    null,
  );
  const [enrollmentAuthMethod, setEnrollmentAuthMethod] = useState("local");
  const [enrollmentAuthUsername, setEnrollmentAuthUsername] = useState("");
  const [enrollmentAuthPassword, setEnrollmentAuthPassword] = useState("");

  const discoveredAuthMethods = enrollmentDiscovery?.authMethods?.length
    ? enrollmentDiscovery.authMethods
    : defaultAuthMethods;
  const selectedAuthMethod = discoveredAuthMethods.find(
    (item) => item.method === enrollmentAuthMethod,
  );
  const centerReadiness = healthStatus?.iWorkerReadiness;
  const enabledLabel = st("enabled", "Enabled");
  const disabledLabel = st("disabled", "Disabled");
  const healthBadgeLabel = healthError
    ? st("healthError", "Check error")
    : !healthStatus
      ? st("healthUnknown", "Not checked")
      : healthStatus.reachable
        ? st("healthOk", "Connected")
        : st("healthUnreachable", "Unreachable");
  const healthSummaryTitle = healthError
    ? st("healthErrorTitle", "Health check returned an error")
    : !healthStatus
      ? st("healthUnknownTitle", "No Center snapshot yet")
      : healthStatus.reachable
        ? st("healthOkTitle", "Center connection is healthy")
        : st("healthUnreachableTitle", "Center is currently unreachable");
  const healthSourceLabel = (source: CenterHealthStatus["source"]) =>
    source === "manual"
      ? st("manualCheck", "Manual check")
      : st("autoCheck", "Auto check after save");

  useEffect(() => {
    if (
      expandedProviderId &&
      !settings.providers.some((provider) => provider.id === expandedProviderId)
    ) {
      setExpandedProviderId(null);
    }
  }, [expandedProviderId, settings.providers]);

  const enabledOptions = (
    <>
      <option value="enabled">{enabledLabel}</option>
      <option value="disabled">{disabledLabel}</option>
    </>
  );

  return (
    <div className="dw-page-stack">
      <section className="card dw-page-panel">
        <div className="dw-panel-header dw-panel-header-compact">
          <div>
            <span className="eyebrow">{st("eyebrow", "Configuration")}</span>
            <h2>{st("title", "Center configuration")}</h2>
          </div>
          <div className="dw-settings-header-meta">
            {dirty ? (
              <span className="dw-settings-dirty-badge">
                {st("dirty", "Unsaved changes")}
              </span>
            ) : (
              <span className="dw-settings-clean-badge">
                {st("clean", "Saved")}
              </span>
            )}
            <small>
              {st(
                "intro",
                "Manage role profile, Center enrollment, authentication, memory, routing, and upstream model services.",
              )}
            </small>
          </div>
        </div>
        <div className="dw-task-layout dw-settings-layout">
          <div className="dw-task-main dw-editor-main">
            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong>
                  <SectionIcon>1</SectionIcon>
                  {st("roleProfile", "Role profile")}
                </strong>
                <span>
                  {st(
                    "roleProfileHint",
                    "Used as the visible identity and default working persona for this iWorker.",
                  )}
                </span>
              </div>
              <div className="dw-form-grid">
                <label>
                  {st("roleName", "Role name")}
                  <input
                    value={settings.roleProfile.name}
                    onChange={(event) => onRoleNameChange(event.target.value)}
                    placeholder={st("roleNamePlaceholder", "Example: Xiao Di")}
                  />
                </label>
                <label>
                  {st("roleDescription", "Role description")}
                  <input
                    value={settings.roleProfile.description}
                    onChange={(event) =>
                      onRoleDescriptionChange(event.target.value)
                    }
                    placeholder={st(
                      "roleDescriptionPlaceholder",
                      "Example: good at minutes, notices, and reporting",
                    )}
                  />
                </label>
              </div>
            </section>

            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong>
                  <SectionIcon>2</SectionIcon>
                  {st("centerConnection", "Center connection")}
                </strong>
                <span>
                  {st(
                    "centerConnectionHint",
                    "Registration, authorization, and Center push are connected here.",
                  )}
                </span>
              </div>
              <div className="dw-settings-group-list">
                <section className="dw-settings-group">
                  <div className="dw-settings-group-head">
                    <strong>{st("baseConnection", "Base connection")}</strong>
                    <span>
                      {st(
                        "baseConnectionHint",
                        "Center address, tenant, department, and local iWorker identity.",
                      )}
                    </span>
                  </div>
                  <div className="dw-form-grid">
                    <label>
                      {st("enableCenter", "Enable Center")}
                      <select
                        value={settings.center.enabled ? "enabled" : "disabled"}
                        onChange={(event) =>
                          onCenterEnabledChange(
                            event.target.value === "enabled",
                          )
                        }
                      >
                        {enabledOptions}
                      </select>
                    </label>
                    <label>
                      {st("host", "Host")}
                      <input
                        value={settings.center.host}
                        onChange={(event) =>
                          onCenterHostChange(event.target.value)
                        }
                        placeholder="127.0.0.1"
                      />
                    </label>
                    <label>
                      {st("port", "Port")}
                      <input
                        value={String(settings.center.port)}
                        onChange={(event) =>
                          onCenterPortChange(event.target.value)
                        }
                        placeholder="9377"
                      />
                    </label>
                    <label>
                      {st("baseUrl", "Base URL")}
                      <input
                        value={settings.center.baseUrl}
                        onChange={(event) =>
                          onCenterBaseUrlChange(event.target.value)
                        }
                        placeholder="http://127.0.0.1:9377"
                      />
                    </label>
                    <label>
                      {st("tenantId", "Tenant ID")}
                      <input
                        value={settings.center.tenantId}
                        onChange={(event) =>
                          onCenterTenantIdChange(event.target.value)
                        }
                        placeholder="default"
                      />
                    </label>
                    <label>
                      {st("departmentId", "Department ID")}
                      <input
                        value={settings.center.departmentId}
                        onChange={(event) =>
                          onCenterDepartmentIdChange(event.target.value)
                        }
                        placeholder="default"
                      />
                    </label>
                    <label>
                      {st("workerId", "Worker ID")}
                      <input
                        value={settings.center.workerId}
                        onChange={(event) =>
                          onCenterWorkerIdChange(event.target.value)
                        }
                        placeholder="local-iworker"
                      />
                    </label>
                    <label>
                      {st("timeoutSec", "Timeout (sec)")}
                      <input
                        value={String(settings.center.timeoutSec)}
                        onChange={(event) =>
                          onCenterTimeoutChange(event.target.value)
                        }
                        placeholder="60"
                      />
                    </label>
                    <label>
                      {st("autoGuardian", "Auto guardian")}
                      <select
                        value={
                          settings.center.goalWatchAutoHandleEnabled
                            ? "enabled"
                            : "disabled"
                        }
                        onChange={(event) =>
                          onGoalWatchAutoHandleEnabledChange(
                            event.target.value === "enabled",
                          )
                        }
                      >
                        {enabledOptions}
                      </select>
                    </label>
                    <label>
                      {st("guardIntervalSec", "Guardian interval (sec)")}
                      <input
                        value={String(settings.center.goalWatchIntervalSec)}
                        onChange={(event) =>
                          onGoalWatchIntervalChange(event.target.value)
                        }
                        placeholder="30"
                      />
                    </label>
                    <label>
                      {st("stuckTimeoutSec", "Stuck timeout (sec)")}
                      <input
                        value={String(settings.center.goalWatchMaxDurationSec)}
                        onChange={(event) =>
                          onGoalWatchMaxDurationChange(event.target.value)
                        }
                        placeholder="120"
                      />
                    </label>
                  </div>
                </section>

                <section className="dw-settings-group dw-settings-enrollment-card">
                  <div className="dw-settings-group-head">
                    <strong>
                      {st("enrollmentTitle", "Center Enrollment")}
                    </strong>
                    <span>
                      {st(
                        "enrollmentHint",
                        "Discover tenants and bind this local body to a Center iWorker.",
                      )}
                    </span>
                  </div>
                  <div className="dw-settings-enrollment-actions">
                    <button
                      type="button"
                      className="secondary"
                      onClick={onDiscoverCenterEnrollment}
                      disabled={
                        loading ||
                        enrollmentDiscovering ||
                        !settings.center.baseUrl.trim()
                      }
                    >
                      {enrollmentDiscovering
                        ? st("discovering", "Discovering...")
                        : st("discoverCenter", "Discover Center")}
                    </button>
                    <small>
                      {settings.center.baseUrl ||
                        st("setBaseUrlFirst", "Set Base URL first")}
                    </small>
                  </div>
                  <div className="dw-settings-enrollment-auth">
                    <label>
                      {st("humanIdentity", "Human identity")}
                      <select
                        value={enrollmentAuthMethod}
                        onChange={(event) =>
                          setEnrollmentAuthMethod(event.target.value)
                        }
                      >
                        {discoveredAuthMethods.map((method) => (
                          <option
                            key={method.method}
                            value={method.method}
                            disabled={!method.enabled && method.implemented}
                          >
                            {st(`authMethod.${method.label}`, method.label)}
                            {method.implemented ? "" : ` (${st("reserved", "reserved")})`}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      {st("usernameEmailPhone", "Username / email / phone")}
                      <input
                        value={enrollmentAuthUsername}
                        onChange={(event) =>
                          setEnrollmentAuthUsername(event.target.value)
                        }
                        placeholder={st("userPlaceholder", "alice@example.com")}
                      />
                    </label>
                    <label>
                      {st("passwordVerificationCode", "Password / verification code")}
                      <input
                        type="password"
                        value={enrollmentAuthPassword}
                        onChange={(event) =>
                          setEnrollmentAuthPassword(event.target.value)
                        }
                        placeholder={st(
                          "passwordPlaceholder",
                          "Required before binding",
                        )}
                      />
                    </label>
                  </div>
                  {selectedAuthMethod ? (
                    <p>{st(`authMethod.${selectedAuthMethod.description}`, selectedAuthMethod.description)}</p>
                  ) : null}
                  {enrollmentMessage ? <p>{enrollmentMessage}</p> : null}
                  {enrollmentError ? <p>{enrollmentError}</p> : null}
                  {enrollmentDiscovery ? (
                    <div className="dw-settings-enrollment-results">
                      <div className="dw-settings-kv-list">
                        <div className="dw-settings-kv-item">
                          <span>{st("tenant", "Tenant")}</span>
                          <strong>
                            {enrollmentDiscovery.selectedTenantId || "-"}
                          </strong>
                        </div>
                        <div className="dw-settings-kv-item">
                          <span>{st("companies", "Companies")}</span>
                          <strong>{enrollmentDiscovery.tenants.length}</strong>
                        </div>
                        <div className="dw-settings-kv-item">
                          <span>{st("roles", "Roles")}</span>
                          <strong>{enrollmentDiscovery.roles.length}</strong>
                        </div>
                        <div className="dw-settings-kv-item">
                          <span>{st("iWorkers", "iWorkers")}</span>
                          <strong>
                            {enrollmentDiscovery.colleagues.length}
                          </strong>
                        </div>
                      </div>
                      <div className="dw-settings-enrollment-list">
                        {enrollmentDiscovery.colleagues.length > 0 ? (
                          enrollmentDiscovery.colleagues.map((worker) => (
                            <article
                              key={worker.id}
                              className="dw-settings-enrollment-worker"
                            >
                              <div>
                                <strong>{worker.name}</strong>
                                <span>
                                  {worker.roleName ||
                                    worker.roleCode ||
                                    st("iWorker", "iWorker")}
                                </span>
                                <p>
                                  {worker.description ||
                                    "No description from Center yet."}
                                </p>
                              </div>
                              <button
                                type="button"
                                className="primary"
                                onClick={() =>
                                  onApplyCenterEnrollment(worker.id, {
                                    method: enrollmentAuthMethod,
                                    username: enrollmentAuthUsername,
                                    password: enrollmentAuthPassword,
                                  })
                                }
                                disabled={
                                  Boolean(enrollmentApplyingId) ||
                                  !enrollmentAuthUsername.trim() ||
                                  !enrollmentAuthPassword.trim()
                                }
                              >
                                {enrollmentApplyingId === worker.id
                                  ? st("binding", "Binding...")
                                  : st("bindHere", "Bind here")}
                              </button>
                            </article>
                          ))
                        ) : (
                          <p>
                            {st(
                              "noWorkers",
                              "No active iWorkers discovered. Apply the enterprise bootstrap plan in iWorkerCenter first.",
                            )}
                          </p>
                        )}
                      </div>
                    </div>
                  ) : null}
                </section>

                <section className="dw-settings-group">
                  <div className="dw-settings-group-head">
                    <strong>{st("routing", "Routing")}</strong>
                    <span>
                      {st(
                        "routingHint",
                        "Controls default upstream service, priority, and failure fallback.",
                      )}
                    </span>
                  </div>
                  <div className="dw-form-grid">
                    <label>
                      {st("routingMode", "Routing mode")}
                      <select
                        value={settings.routing.mode}
                        onChange={(event) =>
                          onRoutingModeChange(
                            event.target
                              .value as DiWorkerSettings["routing"]["mode"],
                          )
                        }
                      >
                        <option value="smart">
                          {st("smartRouting", "Smart routing")}
                        </option>
                        <option value="priority">
                          {st("priorityRouting", "Priority first")}
                        </option>
                        <option value="manual">
                          {st("manualRouting", "Manual default")}
                        </option>
                      </select>
                    </label>
                    <label>
                      {st("defaultProvider", "Default provider")}
                      <input
                        value={settings.routing.defaultProvider}
                        onChange={(event) =>
                          onRoutingDefaultProviderChange(event.target.value)
                        }
                        placeholder="office-openai"
                      />
                    </label>
                    <label className="dw-settings-field-span-2">
                      {st("fallback", "Failure fallback")}
                      <select
                        value={
                          settings.routing.allowFallback
                            ? "enabled"
                            : "disabled"
                        }
                        onChange={(event) =>
                          onRoutingAllowFallbackChange(
                            event.target.value === "enabled",
                          )
                        }
                      >
                        {enabledOptions}
                      </select>
                    </label>
                  </div>
                </section>
              </div>
            </section>

            <section className="card-subtle dw-editor-section dw-settings-section dw-settings-section-compact">
              <div className="dw-pane-head">
                <strong>
                  <SectionIcon>3</SectionIcon>
                  {st("providers", "Providers")}
                </strong>
                <span>
                  {st("servicesCount", "{{count}} services", {
                    count: settings.providers.length,
                  })}
                </span>
              </div>
              <div className="dw-provider-list">
                {settings.providers.map((provider) => {
                  const isExpanded = expandedProviderId === provider.id;
                  const detailsId = `provider-details-${provider.id}`;
                  return (
                    <article
                      key={provider.id}
                      className={`dw-provider-card${isExpanded ? " is-expanded" : ""}`}
                    >
                      <div className="dw-provider-summary">
                        <div className="dw-provider-card-head">
                          <div className="dw-provider-card-title">
                            <strong>{provider.name}</strong>
                            <p>
                              {provider.description ||
                                st("noDescriptionConfigured", "No description configured.")}
                            </p>
                          </div>
                          <div className="dw-provider-card-badges">
                            <span>{provider.protocol.toUpperCase()}</span>
                            <span>
                              {statusText(
                                provider.enabled,
                                enabledLabel,
                                disabledLabel,
                              )}
                            </span>
                          </div>
                        </div>
                        <div className="dw-provider-meta-row">
                          <span>{st("modelWithValue", "Model: {{value}}", { value: provider.model || "-" })}</span>
                          <span>{st("priorityWithValue", "Priority: {{value}}", { value: provider.priority })}</span>
                          <span>
                            {st("contextWithValue", "Context: {{value}}", { value: provider.capabilities.maxContext || 0 })}
                          </span>
                        </div>
                        <div className="dw-provider-feature-row">
                          <span>
                            {provider.capabilities.supportsStream
                              ? st("streaming", "Streaming")
                              : st("nonStreaming", "Non-streaming")}
                          </span>
                          <span>
                            {provider.capabilities.supportsVision
                              ? st("vision", "Vision")
                              : st("textOnly", "Text only")}
                          </span>
                          <span>
                            {provider.features.length
                              ? provider.features.join(" / ")
                              : st("noFeaturesConfigured", "No features configured")}
                          </span>
                        </div>
                        <div className="dw-provider-summary-foot">
                          <span>
                            {isExpanded
                              ? st("editingProvider", "Editing this provider")
                              : st("summaryView", "Summary view")}
                          </span>
                          <button
                            type="button"
                            className="secondary dw-provider-toggle"
                            aria-expanded={isExpanded}
                            aria-controls={detailsId}
                            onClick={() =>
                              setExpandedProviderId((current) =>
                                current === provider.id ? null : provider.id,
                              )
                            }
                          >
                            {isExpanded
                              ? st("collapseProvider", "Collapse {{name}}", { name: provider.name })
                              : st("editProvider", "Edit {{name}}", { name: provider.name })}
                          </button>
                        </div>
                      </div>
                      {isExpanded ? (
                        <div id={detailsId} className="dw-provider-details">
                          <div className="dw-form-grid dw-provider-grid">
                            <label>
                              {st("enabled", "Enabled")}
                              <select
                                value={
                                  provider.enabled ? "enabled" : "disabled"
                                }
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    enabled: event.target.value === "enabled",
                                  })
                                }
                              >
                                {enabledOptions}
                              </select>
                            </label>
                            <label>
                              {st("name", "Name")}
                              <input
                                value={provider.name}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    name: event.target.value,
                                  })
                                }
                              />
                            </label>
                            <label>
                              {st("protocol", "Protocol")}
                              <select
                                value={provider.protocol}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    protocol: event.target
                                      .value as UpstreamProvider["protocol"],
                                  })
                                }
                              >
                                <option value="openai">openai</option>
                                <option value="anthropic">anthropic</option>
                              </select>
                            </label>
                            <label className="dw-provider-field-span-3">
                              {st("baseUrl", "Base URL")}
                              <input
                                value={provider.baseUrl}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    baseUrl: event.target.value,
                                  })
                                }
                              />
                            </label>
                            <label className="dw-provider-field-span-3">
                              {st("apiKey", "API Key")}
                              <input
                                value={provider.apiKey}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    apiKey: event.target.value,
                                  })
                                }
                              />
                            </label>
                          </div>
                          <div className="dw-provider-form-divider" />
                          <div className="dw-form-grid dw-provider-grid dw-provider-grid-secondary">
                            <label>
                              {st("model", "Model")}
                              <input
                                value={provider.model}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    model: event.target.value,
                                  })
                                }
                              />
                            </label>
                            <label>
                              {st("priority", "Priority")}
                              <input
                                value={String(provider.priority)}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    priority: Number(event.target.value) || 0,
                                  })
                                }
                              />
                            </label>
                            <label>
                              {st("maxContext", "Max context")}
                              <input
                                value={String(provider.capabilities.maxContext)}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    capabilities: {
                                      ...provider.capabilities,
                                      maxContext:
                                        Number(event.target.value) || 0,
                                    },
                                  })
                                }
                              />
                            </label>
                            <label className="dw-provider-field-span-3">
                              {st("features", "Features")}
                              <input
                                value={provider.features.join(", ")}
                                onChange={(event) =>
                                  onProviderFeaturesChange(
                                    provider.id,
                                    event.target.value,
                                  )
                                }
                                placeholder={st("featuresPlaceholder", "documents, Chinese, structured")}
                              />
                            </label>
                            <label>
                              {st("streaming", "Streaming")}
                              <select
                                value={
                                  provider.capabilities.supportsStream
                                    ? "enabled"
                                    : "disabled"
                                }
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    capabilities: {
                                      ...provider.capabilities,
                                      supportsStream:
                                        event.target.value === "enabled",
                                    },
                                  })
                                }
                              >
                                {enabledOptions}
                              </select>
                            </label>
                            <label>
                              {st("vision", "Vision")}
                              <select
                                value={
                                  provider.capabilities.supportsVision
                                    ? "enabled"
                                    : "disabled"
                                }
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    capabilities: {
                                      ...provider.capabilities,
                                      supportsVision:
                                        event.target.value === "enabled",
                                    },
                                  })
                                }
                              >
                                {enabledOptions}
                              </select>
                            </label>
                            <label>
                              {st("description", "Description")}
                              <input
                                value={provider.description}
                                onChange={(event) =>
                                  onProviderChange(provider.id, {
                                    description: event.target.value,
                                  })
                                }
                              />
                            </label>
                          </div>
                        </div>
                      ) : null}
                    </article>
                  );
                })}
              </div>
            </section>
          </div>

          <aside className="dw-task-side dw-settings-side">
            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <div className="dw-settings-summary-head">
                <div>
                  <label>{st("memoryStats", "Memory records")}</label>
                  <strong>
                    {memoryStats
                      ? st("recordCount", "{{count}} records", { count: memoryStats.total })
                      : st("notLoaded", "Not loaded")}
                  </strong>
                </div>
                <button
                  type="button"
                  className="secondary"
                  onClick={onRefreshMemoryStats}
                  disabled={
                    loading || memoryStatsLoading || !settings.center.enabled
                  }
                >
                  {memoryStatsLoading
                    ? st("refreshing", "Refreshing...")
                    : st("refreshMemory", "Refresh memory")}
                </button>
              </div>
              <p>
                {st(
                  "memoryCanonical",
                  "Memory is canonical in the registered iWorkerCenter; local storage is only an access cache.",
                )}
              </p>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>{st("companyMemory", "Company memory")}</span>
                  <strong>{memoryStats?.byScope.company ?? 0}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("departmentMemory", "Department memory")}</span>
                  <strong>{memoryStats?.byScope.department ?? 0}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("personalMemory", "Personal memory")}</span>
                  <strong>{memoryStats?.byScope.personal ?? 0}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("currentContext", "Current context")}</span>
                  <strong>{`${settings.center.tenantId || "-"} / ${settings.center.departmentId || "-"} / ${settings.center.workerId || "-"}`}</strong>
                </div>
              </div>
              {memoryStatsError ? <p>{memoryStatsError}</p> : null}
              <div className="dw-settings-save-row">
                <label>
                  {st("memoryScope", "Memory Capture")}
                  <select
                    value={memoryDraftScope}
                    onChange={(event) =>
                      onMemoryDraftScopeChange(event.target.value)
                    }
                    disabled={!settings.center.enabled || memorySaving}
                  >
                    <option value="personal">{st("personalMemory", "Personal memory")}</option>
                    <option value="department">{st("departmentMemory", "Department memory")}</option>
                    <option value="company">{st("companyMemory", "Company memory")}</option>
                  </select>
                </label>
                <label>
                  {st("category", "Category")}
                  <input
                    value={memoryDraftCategory}
                    onChange={(event) =>
                      onMemoryDraftCategoryChange(event.target.value)
                    }
                    placeholder="note"
                    disabled={!settings.center.enabled || memorySaving}
                  />
                </label>
                <label>
                  {st("tags", "Tags")}
                  <input
                    value={memoryDraftTags}
                    onChange={(event) =>
                      onMemoryDraftTagsChange(event.target.value)
                    }
                    placeholder="policy, preference"
                    disabled={!settings.center.enabled || memorySaving}
                  />
                </label>
                <label>
                  {st("content", "Content")}
                  <textarea
                    value={memoryDraftContent}
                    onChange={(event) =>
                      onMemoryDraftContentChange(event.target.value)
                    }
                    placeholder={st("memoryContentPlaceholder", "Write a reusable fact, rule, preference, or handoff note.")}
                    disabled={!settings.center.enabled || memorySaving}
                    rows={4}
                  />
                </label>
                <button
                  type="button"
                  className="primary"
                  onClick={onSaveWorkerMemory}
                  disabled={
                    !settings.center.enabled ||
                    memorySaving ||
                    !memoryDraftContent.trim()
                  }
                >
                  {memorySaving
                    ? st("savingMemory", "Saving memory...")
                    : st("saveMemory", "Save memory")}
                </button>
                <p>
                  {memorySaveMessage ||
                    memorySaveError ||
                    st(
                      "memorySavedHint",
                      "Saved memories are canonical in iWorkerCenter; this computer keeps cache only.",
                    )}
                </p>
              </div>
              <div className="dw-settings-save-row">
                <label>
                  {st("memoryBrowser", "Memory Browser")}
                  <input
                    value={memoryRecallQuery}
                    onChange={(event) =>
                      onMemoryRecallQueryChange(event.target.value)
                    }
                    placeholder={st("memorySearchPlaceholder", "Search registered center memory")}
                    disabled={!settings.center.enabled || memoryRecallLoading}
                  />
                </label>
                <button
                  type="button"
                  className="secondary"
                  onClick={onRecallWorkerMemories}
                  disabled={!settings.center.enabled || memoryRecallLoading}
                >
                  {memoryRecallLoading
                    ? st("recalling", "Recalling...")
                    : st("recallMemories", "Recall memories")}
                </button>
                {memoryRecallError ? <p>{memoryRecallError}</p> : null}
                {memoryDeleteError ? <p>{memoryDeleteError}</p> : null}
                {memoryRecallItems.length > 0 ? (
                  <div className="dw-settings-kv-list">
                    {memoryRecallItems.map((item) => (
                      <div
                        key={item.id || `${item.scope}-${item.content}`}
                        className="dw-settings-kv-item"
                      >
                        <span>{`${item.scope || st("memory", "memory")} / ${item.category || st("note", "note")}`}</span>
                        <strong>{item.content}</strong>
                        <button
                          type="button"
                          className="secondary"
                          onClick={() => onDeleteWorkerMemory(item.id)}
                          disabled={!item.id || memoryDeletingId === item.id}
                        >
                          {memoryDeletingId === item.id
                            ? st("forgetting", "Forgetting...")
                            : st("forget", "Forget")}
                        </button>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p>{st("noRecalledMemory", "No recalled memories yet.")}</p>
                )}
              </div>
            </div>

            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <div className="dw-settings-summary-head">
                <div>
                  <label>
                    {st("connectionStatus", "Connection and status")}
                  </label>
                  <strong>{healthBadgeLabel}</strong>
                </div>
                <button
                  type="button"
                  className="secondary"
                  onClick={onCheckCenterHealth}
                  disabled={loading || healthChecking}
                >
                  {healthChecking
                    ? st("checking", "Checking...")
                    : st("checkCenter", "Test Center connection")}
                </button>
              </div>
              <p>
                {loading
                  ? st("loadingConfig", "Loading configuration...")
                  : settings.center.enabled
                    ? st("centerRoutingEnabled", "Center routing enabled")
                    : st("localDirectMode", "Using local direct mode")}
              </p>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>{st("status", "Status")}</span>
                  <strong>{healthSummaryTitle}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("address", "Address")}</span>
                  <strong>
                    {healthStatus?.resolvedBaseUrl ||
                      settings.center.baseUrl ||
                      st("noCenterAddress", "No Center address configured")}
                  </strong>
                </div>
                {healthStatus ? (
                  <>
                    <div className="dw-settings-kv-item">
                      <span>{st("source", "Source")}</span>
                      <strong>{healthSourceLabel(healthStatus.source)}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>{st("lastChecked", "Last checked")}</span>
                      <strong>{healthStatus.checkedAt}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>{st("providerCount", "Provider count")}</span>
                      <strong>{healthStatus.providerCount}</strong>
                    </div>
                    {healthStatus.configPath ? (
                      <div className="dw-settings-kv-item">
                        <span>{st("configFile", "Config file")}</span>
                        <strong>{healthStatus.configPath}</strong>
                      </div>
                    ) : null}
                  </>
                ) : null}
                {centerReadiness ? (
                  <>
                    <div className="dw-settings-kv-item">
                      <span>{st("iWorkerReadiness", "iWorker readiness")}</span>
                      <strong>
                        {centerReadiness.ready
                          ? st("ready", "ready")
                          : centerReadiness.status || st("needsSetup", "needs setup")}
                      </strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>{st("centerAssets", "Center assets")}</span>
                      <strong>{st("centerAssetsSummary", "{{tenants}} tenants / {{roles}} roles / {{workers}} iWorkers", { tenants: centerReadiness.tenantCount, roles: centerReadiness.roleCount, workers: centerReadiness.colleagueCount })}</strong>
                    </div>
                    <div className="dw-settings-kv-item">
                      <span>{st("humanAuth", "Human auth")}</span>
                      <strong>
                        {centerReadiness.authMethods
                          .map((item) => `${item.method}:${item.status}`)
                          .join(" / ") || st("notReported", "not reported")}
                      </strong>
                    </div>
                  </>
                ) : null}
              </div>
              {centerReadiness?.checks?.length ? (
                <div className="dw-settings-kv-list">
                  {centerReadiness.checks.map((check) => (
                    <div key={check.name} className="dw-settings-kv-item">
                      <span>{check.name}</span>
                      <strong>
                        {check.ready ? st("ready", "ready") : check.status}
                        {typeof check.count === "number"
                          ? ` / ${check.count}`
                          : ""}
                      </strong>
                    </div>
                  ))}
                </div>
              ) : null}
              {healthStatus?.message ? <p>{healthStatus.message}</p> : null}
              {healthError ? <p>{healthError}</p> : null}
            </div>

            <div className="card-subtle dw-side-panel-block dw-settings-summary-card">
              <label>{st("currentConfig", "Current configuration")}</label>
              <div className="dw-settings-kv-list">
                <div className="dw-settings-kv-item">
                  <span>{st("saveStatus", "Save status")}</span>
                  <strong>
                    {dirty
                      ? st("dirty", "Unsaved changes")
                      : st("clean", "Saved")}
                  </strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("role", "Role")}</span>
                  <strong>{settings.roleProfile.name || "-"}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("traits", "Traits")}</span>
                  <strong>{settings.roleProfile.description || "-"}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("defaultProvider", "Default provider")}</span>
                  <strong>{settings.routing.defaultProvider || "-"}</strong>
                </div>
                <div className="dw-settings-kv-item">
                  <span>{st("providerTotal", "Provider count")}</span>
                  <strong>{settings.providers.length}</strong>
                </div>
              </div>
              <div className="dw-settings-save-row">
                <button
                  type="button"
                  className="primary"
                  onClick={onSave}
                  disabled={loading || saving || !dirty}
                >
                  {saving
                    ? st("saving", "Saving...")
                    : dirty
                      ? st("saveConfig", "Save configuration")
                      : st("saved", "Saved")}
                </button>
                <p>
                  {saveMessage ||
                    error ||
                    (dirty
                      ? st(
                          "unsavedHint",
                          "Current changes have not been saved.",
                        )
                      : st(
                          "savedHint",
                          "Current configuration matches the saved version.",
                        ))}
                </p>
              </div>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
