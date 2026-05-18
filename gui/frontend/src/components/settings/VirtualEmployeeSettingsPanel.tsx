import { useEffect, useRef, useState } from "react";
import { EventsOn, EventsOff, BrowserOpenURL } from "../../../wailsjs/runtime";
import {
  RegisterVirtualEmployee,
  UpdateVESettings,
  GetVEStatus,
  GetDigitalEmployeeSensitiveQueryPolicy,
  SaveDigitalEmployeeSensitiveQueryPolicy,
  SelectVEAllowedDirectory,
  GetVEAllowedDirectories,
  SetVEAllowedDirectories,
  LoadConfig,
} from "../../../wailsjs/go/main/App";
import { VEApprovalCapabilitySection } from "./VEApprovalCapabilitySection";

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
    whitelist?: string[];
    blacklist?: string[];
  };
};

export type AccessPolicy = "public" | "whitelist" | "blacklist" | "per_request";
export type VEStatus = "pending" | "active" | "disabled" | "rejected";
type SensitiveQueryPolicy = "confirm" | "deny" | "allow";

type VEFormStateSetter = {
  setRegistered: (value: boolean) => void;
  setStatus: (value: VEStatus | null) => void;
  setName: (value: string) => void;
  setSkillDescription: (value: string) => void;
  setAccessPolicy: (value: AccessPolicy | "") => void;
  setWhitelist: (value: string[]) => void;
  setBlacklist: (value: string[]) => void;
  setListInput: (value: string) => void;
  setNameError: (value: string) => void;
  setSkillError: (value: string) => void;
  setPolicyError: (value: string) => void;
};

function resetVEFormState(setter: VEFormStateSetter) {
  setter.setRegistered(false);
  setter.setStatus(null);
  setter.setName("");
  setter.setSkillDescription("");
  setter.setAccessPolicy("");
  setter.setWhitelist([]);
  setter.setBlacklist([]);
  setter.setListInput("");
  setter.setNameError("");
  setter.setSkillError("");
  setter.setPolicyError("");
}

type Props = {
  remoteMachineId: string;
  lang?: string;
};

const textForLang = (
  lang: string | undefined,
  en: string,
  zhHans: string,
  zhHant = zhHans,
) => (lang === "en" ? en : lang === "zh-Hant" ? zhHant : zhHans);

const STATUS_COLORS: Record<VEStatus, string> = {
  pending: "#f59e0b",
  active: "#10b981",
  disabled: "#6b7280",
  rejected: "#ef4444",
};

const statusLabel = (status: VEStatus, lang?: string) =>
  ({
    pending: textForLang(
      lang,
      "Pending review",
      "\u5ba1\u6838\u4e2d",
      "\u5be9\u6838\u4e2d",
    ),
    active: textForLang(
      lang,
      "Active",
      "\u5df2\u6fc0\u6d3b",
      "\u5df2\u555f\u7528",
    ),
    disabled: textForLang(
      lang,
      "Disabled",
      "\u5df2\u7981\u7528",
      "\u5df2\u505c\u7528",
    ),
    rejected: textForLang(
      lang,
      "Rejected",
      "\u5df2\u62d2\u7edd",
      "\u5df2\u62d2\u7d55",
    ),
  })[status];

const policyOptions = (
  lang?: string,
): { value: AccessPolicy; label: string }[] => [
  {
    value: "public",
    label: textForLang(
      lang,
      "Anyone can access",
      "\u6240\u6709\u4eba\u53ef\u8bbf\u95ee",
      "\u6240\u6709\u4eba\u53ef\u8a2a\u554f",
    ),
  },
  {
    value: "whitelist",
    label: textForLang(
      lang,
      "Whitelist only",
      "\u4ec5\u767d\u540d\u5355\u53ef\u8bbf\u95ee",
      "\u50c5\u767d\u540d\u55ae\u53ef\u8a2a\u554f",
    ),
  },
  {
    value: "blacklist",
    label: textForLang(
      lang,
      "Blacklist blocked",
      "\u9ed1\u540d\u5355\u4e0d\u53ef\u8bbf\u95ee",
      "\u9ed1\u540d\u55ae\u4e0d\u53ef\u8a2a\u554f",
    ),
  },
  {
    value: "per_request",
    label: textForLang(
      lang,
      "Authorize each request",
      "\u6bcf\u6b21\u8bbf\u95ee\u9700\u6388\u6743",
      "\u6bcf\u6b21\u8a2a\u554f\u9700\u6388\u6b0a",
    ),
  },
];

const copy = (lang?: string) => ({
  title: textForLang(
    lang,
    "Digital Employee Settings",
    "\u6570\u5b57\u5458\u5de5\u8bbe\u7f6e",
    "\u6578\u5b57\u54e1\u5de5\u8a2d\u7f6e",
  ),
  approved: textForLang(
    lang,
    "Your digital employee registration has been approved.",
    "\u60a8\u7684\u6570\u5b57\u5458\u5de5\u6ce8\u518c\u5df2\u901a\u8fc7\u5ba1\u6279\uff01",
    "\u60a8\u7684\u6578\u5b57\u54e1\u5de5\u8a3b\u518a\u5df2\u901a\u904e\u5be9\u6279\uff01",
  ),
  rejectedHint: textForLang(
    lang,
    "Your registration was rejected. You can modify and resubmit.",
    "\u60a8\u7684\u6ce8\u518c\u7533\u8bf7\u5df2\u88ab\u62d2\u7edd\uff0c\u53ef\u4fee\u6539\u540e\u91cd\u65b0\u7533\u8bf7\u3002",
    "\u60a8\u7684\u8a3b\u518a\u7533\u8acb\u5df2\u88ab\u62d2\u7d55\uff0c\u53ef\u4fee\u6539\u5f8c\u91cd\u65b0\u7533\u8acb\u3002",
  ),
  reapply: textForLang(
    lang,
    "Reapply",
    "\u91cd\u65b0\u7533\u8bf7",
    "\u91cd\u65b0\u7533\u8acb",
  ),
  pendingHint: textForLang(
    lang,
    "Your registration is under review. Please wait for the administrator to approve.",
    "\u60a8\u7684\u6ce8\u518c\u7533\u8bf7\u6b63\u5728\u5ba1\u6838\u4e2d\uff0c\u8bf7\u7b49\u5f85\u7ba1\u7406\u5458\u5ba1\u6279\u3002",
    "\u60a8\u7684\u8a3b\u518a\u7533\u8acb\u6b63\u5728\u5be9\u6838\u4e2d\uff0c\u8acb\u7b49\u5f85\u7ba1\u7406\u54e1\u5be9\u6279\u3002",
  ),
  name: textForLang(lang, "Name", "\u540d\u79f0", "\u540d\u7a31"),
  namePlaceholder: textForLang(
    lang,
    "Digital employee name",
    "\u6570\u5b57\u5458\u5de5\u540d\u79f0",
    "\u6578\u5b57\u54e1\u5de5\u540d\u7a31",
  ),
  skill: textForLang(
    lang,
    "Skill description",
    "\u6280\u80fd\u63cf\u8ff0",
    "\u6280\u80fd\u63cf\u8ff0",
  ),
  skillPlaceholder: textForLang(
    lang,
    "Describe the digital employee skills and abilities",
    "\u63cf\u8ff0\u6570\u5b57\u5458\u5de5\u7684\u6280\u80fd\u548c\u80fd\u529b",
    "\u63cf\u8ff0\u6578\u5b57\u54e1\u5de5\u7684\u6280\u80fd\u548c\u80fd\u529b",
  ),
  accessPolicy: textForLang(
    lang,
    "Access policy",
    "\u8bbf\u95ee\u7b56\u7565",
    "\u8a2a\u554f\u7b56\u7565",
  ),
  choose: textForLang(
    lang,
    "Choose",
    "\u8bf7\u9009\u62e9",
    "\u8acb\u9078\u64c7",
  ),
  whitelist: textForLang(
    lang,
    "Whitelist",
    "\u767d\u540d\u5355",
    "\u767d\u540d\u55ae",
  ),
  blacklist: textForLang(
    lang,
    "Blacklist",
    "\u9ed1\u540d\u5355",
    "\u9ed1\u540d\u55ae",
  ),
  listPlaceholder: textForLang(
    lang,
    "Enter user identifier",
    "\u8f93\u5165\u7528\u6237\u6807\u8bc6",
    "\u8f38\u5165\u7528\u6236\u6a19\u8b58",
  ),
  add: textForLang(lang, "Add", "\u6dfb\u52a0", "\u65b0\u589e"),
  remove: textForLang(lang, "Remove", "\u79fb\u9664", "\u79fb\u9664"),
  sensitiveTitle: textForLang(
    lang,
    "Password or sensitive information query",
    "\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2",
    "\u5bc6\u78bc\u6216\u654f\u611f\u8cc7\u8a0a\u67e5\u8a62",
  ),
  confirm: textForLang(
    lang,
    "Human confirmation",
    "\u4eba\u5de5\u786e\u8ba4",
    "\u4eba\u5de5\u78ba\u8a8d",
  ),
  deny: textForLang(lang, "Deny", "\u62d2\u7edd", "\u62d2\u7d55"),
  allow: textForLang(
    lang,
    "Allow automatically",
    "\u81ea\u52a8\u5141\u8bb8",
    "\u81ea\u52d5\u5141\u8a31",
  ),
  sensitiveHint: textForLang(
    lang,
    "Default is human confirmation. When enabled, the digital employee waits for local human permission and denies by default after 1 minute with no response.",
    "\u9ed8\u8ba4\u4eba\u5de5\u786e\u8ba4\u3002\u9009\u62e9\u4eba\u5de5\u786e\u8ba4\u65f6\uff0c\u6570\u5b57\u5458\u5de5\u9047\u5230\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2\u4f1a\u7b49\u5f85\u672c\u5730\u4eba\u7c7b\u5458\u5de5\u8bb8\u53ef\uff0c1 \u5206\u949f\u65e0\u54cd\u5e94\u5219\u9ed8\u8ba4\u62d2\u7edd\u3002",
    "\u9810\u8a2d\u70ba\u4eba\u5de5\u78ba\u8a8d\u3002\u9078\u64c7\u4eba\u5de5\u78ba\u8a8d\u6642\uff0c\u6578\u5b57\u54e1\u5de5\u9047\u5230\u5bc6\u78bc\u6216\u654f\u611f\u8cc7\u8a0a\u67e5\u8a62\u6703\u7b49\u5f85\u672c\u5730\u4eba\u985e\u54e1\u5de5\u8a31\u53ef\uff0c1 \u5206\u9418\u7121\u56de\u61c9\u5247\u9810\u8a2d\u62d2\u7d55\u3002",
  ),
  update: textForLang(
    lang,
    "Update settings",
    "\u66f4\u65b0\u8bbe\u7f6e",
    "\u66f4\u65b0\u8a2d\u7f6e",
  ),
  register: textForLang(
    lang,
    "Register digital employee",
    "\u6ce8\u518c\u6570\u5b57\u5458\u5de5",
    "\u8a3b\u518a\u6578\u5b57\u54e1\u5de5",
  ),
  nameRequired: textForLang(
    lang,
    "Name is required",
    "\u540d\u79f0\u4e0d\u80fd\u4e3a\u7a7a",
    "\u540d\u7a31\u4e0d\u80fd\u70ba\u7a7a",
  ),
  nameTooLong: textForLang(
    lang,
    "Name cannot exceed 50 characters",
    "\u540d\u79f0\u4e0d\u80fd\u8d85\u8fc7 50 \u4e2a\u5b57\u7b26",
    "\u540d\u7a31\u4e0d\u80fd\u8d85\u904e 50 \u500b\u5b57\u5143",
  ),
  skillRequired: textForLang(
    lang,
    "Skill description is required",
    "\u6280\u80fd\u63cf\u8ff0\u4e0d\u80fd\u4e3a\u7a7a",
    "\u6280\u80fd\u63cf\u8ff0\u4e0d\u80fd\u70ba\u7a7a",
  ),
  skillTooLong: textForLang(
    lang,
    "Skill description cannot exceed 500 characters",
    "\u6280\u80fd\u63cf\u8ff0\u4e0d\u80fd\u8d85\u8fc7 500 \u4e2a\u5b57\u7b26",
    "\u6280\u80fd\u63cf\u8ff0\u4e0d\u80fd\u8d85\u904e 500 \u500b\u5b57\u5143",
  ),
  policyRequired: textForLang(
    lang,
    "Choose an access policy",
    "\u8bf7\u9009\u62e9\u8bbf\u95ee\u7b56\u7565",
    "\u8acb\u9078\u64c7\u8a2a\u554f\u7b56\u7565",
  ),
});

const normalizeSensitivePolicy = (policy: string): SensitiveQueryPolicy => {
  const normalized = String(policy || "")
    .trim()
    .toLowerCase();
  return normalized === "deny" ||
    normalized === "allow" ||
    normalized === "confirm"
    ? normalized
    : "confirm";
};

export function VirtualEmployeeSettingsPanel({ remoteMachineId, lang }: Props) {
  const c = copy(lang);
  const [name, setName] = useState("");
  const [skillDescription, setSkillDescription] = useState("");
  const [accessPolicy, setAccessPolicy] = useState<AccessPolicy | "">("");
  const [whitelist, setWhitelist] = useState<string[]>([]);
  const [blacklist, setBlacklist] = useState<string[]>([]);
  const [status, setStatus] = useState<VEStatus | null>(null);
  const [registered, setRegistered] = useState(false);
  const [listInput, setListInput] = useState("");
  const [approvalNotice, setApprovalNotice] = useState("");
  const [sensitiveQueryPolicy, setSensitiveQueryPolicy] =
    useState<SensitiveQueryPolicy>("confirm");
  const [nameError, setNameError] = useState("");
  const [skillError, setSkillError] = useState("");
  const [policyError, setPolicyError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const mountedRef = useRef(true);

  // --- Allowed Directories state ---
  const [allowedDirs, setAllowedDirs] = useState<string[]>([]);
  const [dirDuplicateWarning, setDirDuplicateWarning] = useState("");

  async function loadStatus() {
    try {
      const resp = (await GetVEStatus()) as VEStatusResponse;
      if (!mountedRef.current) return;
      if (resp?.registered && resp.employee) {
        setRegistered(true);
        setName(resp.employee.name || "");
        setSkillDescription(resp.employee.skill_description || "");
        setAccessPolicy((resp.employee.access_policy as AccessPolicy) || "");
        setWhitelist(Array.isArray(resp.employee.whitelist) ? resp.employee.whitelist : []);
        setBlacklist(Array.isArray(resp.employee.blacklist) ? resp.employee.blacklist : []);
        setStatus((resp.employee.status as VEStatus) || null);
      } else {
        resetVEFormState({
        setRegistered,
        setStatus,
        setName,
        setSkillDescription,
        setAccessPolicy,
        setWhitelist,
        setBlacklist,
        setListInput,
        setNameError,
        setSkillError,
        setPolicyError,
      });
      }
    } catch {
      if (!mountedRef.current) return;
      resetVEFormState({
          setRegistered,
          setStatus,
          setName,
          setSkillDescription,
          setAccessPolicy,
          setWhitelist,
          setBlacklist,
          setListInput,
          setNameError,
          setSkillError,
          setPolicyError,
        });
    }
  }

  useEffect(() => {
    mountedRef.current = true;
    if (remoteMachineId) void loadStatus();
    return () => {
      mountedRef.current = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [remoteMachineId]);

  useEffect(() => {
    let cancelled = false;
    GetDigitalEmployeeSensitiveQueryPolicy()
      .then((policy) => {
        if (!cancelled) setSensitiveQueryPolicy(normalizeSensitivePolicy(policy));
      })
      .catch(() => {
        if (!cancelled) setSensitiveQueryPolicy("confirm");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Load allowed directories on mount (Requirement 2.2)
  useEffect(() => {
    let cancelled = false;
    GetVEAllowedDirectories()
      .then((dirs) => {
        if (!cancelled) setAllowedDirs(Array.isArray(dirs) ? dirs : []);
      })
      .catch(() => {
        if (!cancelled) setAllowedDirs([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Check if a path is a duplicate (case-insensitive on Windows)
  function isDuplicateDir(newPath: string, existingDirs: string[]): boolean {
    const normalized = newPath.toLowerCase().replace(/\//g, "\\");
    return existingDirs.some(
      (d) => d.toLowerCase().replace(/\//g, "\\") === normalized,
    );
  }

  async function handleAddDirectory() {
    setDirDuplicateWarning("");
    try {
      const selected = await SelectVEAllowedDirectory();
      if (!selected) return; // User cancelled (Requirement 1.4)
      if (isDuplicateDir(selected, allowedDirs)) {
        // Requirement 1.6: duplicate detection with warning
        setDirDuplicateWarning(
          textForLang(
            lang,
            `Directory "${selected}" is already in the list.`,
            `目录 "${selected}" 已在列表中。`,
            `目錄 "${selected}" 已在列表中。`,
          ),
        );
        return;
      }
      const updated = [...allowedDirs, selected];
      await SetVEAllowedDirectories(updated);
      setAllowedDirs(updated);
    } catch {
      // Directory picker failed to open (Requirement 7.4)
    }
  }

  async function handleRemoveDirectory(dir: string) {
    const updated = allowedDirs.filter((d) => d !== dir);
    try {
      await SetVEAllowedDirectories(updated);
      setAllowedDirs(updated);
      setDirDuplicateWarning("");
    } catch {
      // Persist failed — keep UI unchanged
    }
  }

  async function handleSensitiveQueryPolicyChange(value: SensitiveQueryPolicy) {
    const previous = sensitiveQueryPolicy;
    setSensitiveQueryPolicy(value);
    try {
      await SaveDigitalEmployeeSensitiveQueryPolicy(value);
    } catch {
      setSensitiveQueryPolicy(previous);
    }
  }

  const approvedTextRef = useRef(c.approved);
  approvedTextRef.current = c.approved;

  useEffect(() => {
    if (!remoteMachineId) return;
    const unsub = EventsOn("ve:approved", () => {
      void loadStatus();
      if (mountedRef.current) {
        setApprovalNotice(approvedTextRef.current);
        window.setTimeout(() => {
          if (mountedRef.current) setApprovalNotice("");
        }, 8000);
      }
    });
    const unsubRejected = EventsOn("ve:rejected", () => {
      void loadStatus();
    });
    const unsubDisabled = EventsOn("ve:disabled", () => {
      void loadStatus();
    });
    return () => {
      if (typeof unsub === "function") unsub();
      else EventsOff("ve:approved");
      if (typeof unsubRejected === "function") unsubRejected();
      else EventsOff("ve:rejected");
      if (typeof unsubDisabled === "function") unsubDisabled();
      else EventsOff("ve:disabled");
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [remoteMachineId]);

  if (!remoteMachineId) return null;

  const isRejected = status === "rejected";
  const isPending = status === "pending";
  // When rejected, the form is editable for reapply; when pending, the form is read-only.
  const formDisabled = isPending;

  function validateName(value: string): string {
    if (!value.trim()) return c.nameRequired;
    if (value.length > 50) return c.nameTooLong;
    return "";
  }

  function validateSkillDescription(value: string): string {
    if (!value.trim()) return c.skillRequired;
    if (value.length > 500) return c.skillTooLong;
    return "";
  }

  function validatePolicy(value: string): string {
    return value ? "" : c.policyRequired;
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

  async function handleSubmit() {
    if (!validate()) return;
    setSubmitting(true);
    const list =
      accessPolicy === "whitelist"
        ? whitelist
        : accessPolicy === "blacklist"
          ? blacklist
          : [];
    try {
      if (registered && !isRejected) {
        await UpdateVESettings(name, skillDescription, accessPolicy, list);
      } else {
        await RegisterVirtualEmployee(
          name,
          skillDescription,
          accessPolicy,
          list,
        );
        setRegistered(true);
        setStatus("pending");
      }
      await loadStatus();
    } catch (err: any) {
      const msg = err?.message || String(err || "");
      if (msg) {
        setNameError(msg);
      }
    } finally {
      if (mountedRef.current) setSubmitting(false);
    }
  }

  function handleAddToList() {
    const item = listInput.trim();
    if (!item) return;
    if (accessPolicy === "whitelist" && !whitelist.includes(item))
      setWhitelist([...whitelist, item]);
    if (accessPolicy === "blacklist" && !blacklist.includes(item))
      setBlacklist([...blacklist, item]);
    setListInput("");
  }

  function handleRemoveFromList(item: string) {
    if (accessPolicy === "whitelist")
      setWhitelist(whitelist.filter((i) => i !== item));
    if (accessPolicy === "blacklist")
      setBlacklist(blacklist.filter((i) => i !== item));
  }

  const showListEditor =
    accessPolicy === "whitelist" || accessPolicy === "blacklist";

  return (
    <div data-testid="ve-settings-panel" className="ve-settings-panel">
      <div className="ve-settings-header">
        <h3 className="ve-settings-title">{c.title}</h3>
        {status && (
          <span
            data-testid="ve-status-badge"
            className="ve-status-badge"
            style={{ color: STATUS_COLORS[status], borderColor: STATUS_COLORS[status] }}
          >
            {statusLabel(status, lang)}
          </span>
        )}
      </div>

      {approvalNotice && (
        <div className="ve-notice ve-notice--success" role="status">
          {approvalNotice}
        </div>
      )}
      {isPending && (
        <div className="ve-notice ve-notice--warning" role="status">
          {c.pendingHint}
        </div>
      )}
      {isRejected && (
        <div className="ve-notice ve-notice--error" role="alert">
          {c.rejectedHint}
        </div>
      )}

      <div className="ve-form">
        <div className="ve-form-row">
          <label className="ve-form-label" htmlFor="ve-name">{c.name}</label>
          <div className="ve-form-field">
            <input
              id="ve-name"
              className="ve-form-input"
              type="text"
              value={name}
              maxLength={50}
              onChange={(e) => {
                setName(e.target.value);
                setNameError(validateName(e.target.value));
              }}
              placeholder={c.namePlaceholder}
              disabled={formDisabled}
            />
            {nameError && (
              <span data-testid="name-error" role="alert" className="ve-form-error">
                {nameError}
              </span>
            )}
          </div>
        </div>

        <div className="ve-form-row ve-form-row--top">
          <label className="ve-form-label" htmlFor="ve-skill">{c.skill}</label>
          <div className="ve-form-field">
            <textarea
              id="ve-skill"
              className="ve-form-input ve-form-textarea"
              value={skillDescription}
              maxLength={500}
              rows={3}
              onChange={(e) => {
                setSkillDescription(e.target.value);
                setSkillError(validateSkillDescription(e.target.value));
              }}
              placeholder={c.skillPlaceholder}
              disabled={formDisabled}
            />
            {skillError && (
              <span data-testid="skill-error" role="alert" className="ve-form-error">
                {skillError}
              </span>
            )}
          </div>
        </div>

        <div className="ve-form-row-dual">
          <div className="ve-form-row-dual-item">
            <label className="ve-form-label" htmlFor="ve-policy">{c.accessPolicy}</label>
            <select
              id="ve-policy"
              className="ve-form-input ve-form-select"
              value={accessPolicy}
              onChange={(e) => {
                const val = e.target.value as AccessPolicy | "";
                setAccessPolicy(val);
                setPolicyError(validatePolicy(val));
              }}
            >
              <option value="">{c.choose}</option>
              {policyOptions(lang).map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </select>
            {policyError && (
              <span data-testid="policy-error" role="alert" className="ve-form-error">
                {policyError}
              </span>
            )}
          </div>
          <div className="ve-form-row-dual-item">
            <label className="ve-form-label" htmlFor="ve-sensitive-policy">{c.sensitiveTitle}</label>
            <select
              id="ve-sensitive-policy"
              className="ve-form-input ve-form-select"
              value={sensitiveQueryPolicy}
              onChange={(e) =>
                handleSensitiveQueryPolicyChange(
                  e.target.value as SensitiveQueryPolicy,
                )
              }
            >
              <option value="confirm">{c.confirm}</option>
              <option value="deny">{c.deny}</option>
              <option value="allow">{c.allow}</option>
            </select>
          </div>
        </div>

        <p className="ve-form-hint ve-sensitive-hint">{c.sensitiveHint}</p>

        {/* 允许访问目录 section (Requirements 1.1-1.8, 2.2) */}
        <div data-testid="ve-allowed-dirs-section" className="ve-form-group">
          <div className="ve-dirs-header">
            <label className="ve-form-label">
              {textForLang(lang, "Allowed Access Directories", "允许访问目录", "允許存取目錄")}
            </label>
            <button
              className="ve-btn ve-btn--link"
              onClick={handleAddDirectory}
              data-testid="ve-add-dir-btn"
            >
              {textForLang(lang, "Add Directory", "添加目录", "新增目錄")}
            </button>
          </div>
          {dirDuplicateWarning && (
            <div
              className="ve-notice ve-notice--warning"
              role="alert"
              data-testid="ve-dir-duplicate-warning"
              style={{ marginTop: 4, marginBottom: 4 }}
            >
              {dirDuplicateWarning}
            </div>
          )}
          {allowedDirs.length > 0 && (
            <ul data-testid="ve-allowed-dirs-list" className="ve-list-items">
              {allowedDirs.map((dir, idx) => (
                <li key={dir} className="ve-list-item">
                  <span className="ve-list-item-text" title={dir}>{dir}</span>
                  <button
                    className="ve-btn ve-btn--ghost"
                    onClick={() => handleRemoveDirectory(dir)}
                    data-testid={`ve-remove-dir-${idx}`}
                    aria-label={textForLang(lang, `Remove ${dir}`, `删除 ${dir}`, `刪除 ${dir}`)}
                  >
                    ✕
                  </button>
                </li>
              ))}
            </ul>
          )}
          {allowedDirs.length === 0 && (
            <p className="ve-form-hint" data-testid="ve-dirs-empty-hint">
              {textForLang(
                lang,
                "No directories configured. The VE cannot send files until at least one directory is added.",
                "未配置目录。数字员工在添加至少一个目录前无法发送文件。",
                "未設定目錄。數字員工在新增至少一個目錄前無法傳送檔案。",
              )}
            </p>
          )}
        </div>

        {/* Approval Capability Section (Requirements 3.1, 3.4, 3.5, 3.6, 3.7) */}
        <VEApprovalCapabilitySection lang={lang} />

        {/* Approval Workflow Design Button (Requirement 1.1) */}
        <div data-testid="ve-approval-workflow-design-section" className="ve-form-group">
          <div className="ve-form-row">
            <label className="ve-form-label">
              {textForLang(lang, "Approval Workflow", "审批工作流", "審批工作流")}
            </label>
            <div className="ve-form-field">
              <button
                className="ve-btn ve-btn--secondary"
                data-testid="ve-approval-workflow-design-btn"
                onClick={async () => {
                  try {
                    const cfg = await LoadConfig() as { remote_hub_url?: string } | null;
                    const hubUrl = (cfg?.remote_hub_url || "").replace(/\/+$/, "");
                    if (hubUrl) {
                      BrowserOpenURL(`${hubUrl}/approval_workflow`);
                    }
                  } catch {
                    // Config load failed — ignore silently
                  }
                }}
              >
                {textForLang(lang, "Approval Workflow Design", "审批工作流设计", "審批工作流設計")}
              </button>
              <p className="ve-form-hint">
                {textForLang(
                  lang,
                  "Open the visual workflow designer on Hub to create and edit approval workflows.",
                  "打开 Hub 上的可视化工作流设计器，创建和编辑审批工作流。",
                  "開啟 Hub 上的視覺化工作流設計器，建立和編輯審批工作流。",
                )}
              </p>
            </div>
          </div>
        </div>

        {showListEditor && !formDisabled && (
          <div data-testid="list-editor" className="ve-form-group">
            <label className="ve-form-label">
              {accessPolicy === "whitelist" ? c.whitelist : c.blacklist}
            </label>
            <div className="ve-list-input-row">
              <input
                data-testid="list-input"
                className="ve-form-input"
                type="text"
                value={listInput}
                onChange={(e) => setListInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleAddToList();
                }}
                placeholder={c.listPlaceholder}
              />
              <button className="ve-btn ve-btn--secondary" onClick={handleAddToList} data-testid="list-add-btn">
                {c.add}
              </button>
            </div>
            <ul data-testid="list-items" className="ve-list-items">
              {(accessPolicy === "whitelist" ? whitelist : blacklist).map(
                (item) => (
                  <li key={item} className="ve-list-item">
                    <span className="ve-list-item-text">{item}</span>
                    <button
                      className="ve-btn ve-btn--ghost"
                      onClick={() => handleRemoveFromList(item)}
                      data-testid={"remove-" + item}
                    >
                      {c.remove}
                    </button>
                  </li>
                ),
              )}
            </ul>
          </div>
        )}

        {!isPending && (
          <div className="ve-form-actions">
            <button
              className="ve-btn ve-btn--primary"
              onClick={handleSubmit}
              disabled={submitting}
              data-testid="ve-submit-btn"
            >
              {submitting ? "..." : isRejected ? c.reapply : registered ? c.update : c.register}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
