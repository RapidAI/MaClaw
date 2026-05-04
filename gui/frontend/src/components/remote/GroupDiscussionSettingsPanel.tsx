import type { CSSProperties, ReactNode } from "react";
import { useEffect, useRef, useState } from "react";
import { main } from "../../../wailsjs/go/models";
import { colors, radius } from "./styles";

type Props = { config: main.AppConfig | null; saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void; lang: string; };
type GroupDiscussionConfig = { enabled?: boolean; discoverable?: boolean; availability?: string; suggest_consultation?: boolean; confirm_before_start?: boolean; display_name?: string; security_group_id?: string; skills?: string[]; description?: string; model_visibility?: string; languages?: string[]; invite_policy?: string; allow_security_group_free_discussion?: boolean; allowed_roles?: string[]; max_risk_level?: string; context_policy?: string; reject_when_dnd?: boolean; max_rounds?: number; timeout_seconds?: number; concurrent_limit?: number; };
const DEFAULT_NAME = "MaClaw";
const DEFAULT_ROLE_DESCRIPTION = "一个尽心尽责无所不能的软件开发管家";
const defaults: Required<GroupDiscussionConfig> = { enabled: true, discoverable: true, availability: "available", suggest_consultation: true, confirm_before_start: true, display_name: "", security_group_id: "", skills: [], description: "", model_visibility: "class_only", languages: ["zh-Hans"], invite_policy: "ask_always", allow_security_group_free_discussion: false, allowed_roles: ["observe", "speak", "review"], max_risk_level: "medium", context_policy: "summary_only", reject_when_dnd: true, max_rounds: 3, timeout_seconds: 300, concurrent_limit: 1 };
const copy = {
  en: { title: "Current-Hub Group Discussion", intro: "Let this MaClaw appear on the current Hub expert list, receive invitations, and suggest a consultation when a task becomes complex. Discussions never leave the current Hub.", on: "On", off: "Off", basic: "Basic", basicDesc: "Control whether this MaClaw can be found and when it may initiate discussions.", listed: "Show on current-Hub expert list", suggest: "Allow MaClaw to suggest starting a discussion", confirm: "Ask before starting", dnd: "Reject invites while do-not-disturb", status: "Availability", available: "Available", busy: "Busy", dndStatus: "Do not disturb", identity: "Expert Identity", identityDesc: "This identity is shown to other MaClaw experts on the same Hub.", name: "Expert name", skills: "Specialties", skillsHint: "Separate multiple skills with commas", securityGroup: "Security group ID", securityGroupHint: "Required for same-security-group free discussion", modelVisibility: "Model visibility", hidden: "Hidden", classOnly: "Capability class only", providerAlias: "Provider and alias", description: "Specialty description", invite: "Invites and Limits", inviteDesc: "Define how invitations are handled and how much task context may be shared.", invitePolicy: "When receiving invites", askAlways: "Ask every time", autoTrusted: "Auto-accept trusted peers", observeOnly: "Auto-accept observe-only", rejectAll: "Reject all", sameSecurityGroup: "Allow same-security-group peers to discuss freely", contextPolicy: "Context scope", summaryOnly: "Summary only", summarySnippets: "Summary and necessary snippets", fullContext: "Full context", maxRisk: "Max risk", low: "Low", medium: "Medium", high: "High", maxRounds: "Max rounds", timeout: "Timeout seconds", concurrent: "Concurrent limit", note: "Free discussion is still limited to the current Hub and the same security group, and remains constrained by DND, risk, context, and concurrency limits.", roles: "Allowed roles", rolesDesc: "Choose what this MaClaw may do when participating in group discussions.", observe: "Observe", speak: "Speak", propose: "Propose", review: "Review", vote: "Vote", saved: "Saved", enabledHint: "Ready for current-Hub collaboration", disabledHint: "Hidden from discovery and invitations" },
  zhHans: { title: "\u5f53\u524d Hub \u7fa4\u7ec4\u8ba8\u8bba", intro: "\u8ba9\u6b64 MaClaw \u51fa\u73b0\u5728\u5f53\u524d Hub \u7684\u4e13\u5bb6\u699c\u4e2d\uff0c\u63a5\u6536\u9080\u8bf7\uff0c\u5e76\u5728\u590d\u6742\u4efb\u52a1\u4e2d\u5efa\u8bae\u53d1\u8d77\u4f1a\u8bca\u3002\u8ba8\u8bba\u4e0d\u4f1a\u79bb\u5f00\u5f53\u524d Hub\u3002", on: "\u5df2\u5f00\u542f", off: "\u5df2\u5173\u95ed", basic: "\u57fa\u7840", basicDesc: "\u63a7\u5236\u662f\u5426\u53ef\u88ab\u53d1\u73b0\uff0c\u4ee5\u53ca MaClaw \u4f55\u65f6\u53ef\u4ee5\u5efa\u8bae\u53d1\u8d77\u8ba8\u8bba\u3002", listed: "\u663e\u793a\u5728\u5f53\u524d Hub \u4e13\u5bb6\u699c", suggest: "\u5141\u8bb8 MaClaw \u5efa\u8bae\u53d1\u8d77\u8ba8\u8bba", confirm: "\u53d1\u8d77\u524d\u9700\u8981\u786e\u8ba4", dnd: "\u52ff\u6270\u65f6\u81ea\u52a8\u62d2\u7edd\u9080\u8bf7", status: "\u5f53\u524d\u72b6\u6001", available: "\u53ef\u7528", busy: "\u5fd9\u788c", dndStatus: "\u52ff\u6270", identity: "\u4e13\u5bb6\u8eab\u4efd", identityDesc: "\u8fd9\u4e2a\u8eab\u4efd\u4f1a\u5c55\u793a\u7ed9\u540c\u4e00 Hub \u4e0a\u7684\u5176\u4ed6 MaClaw \u4e13\u5bb6\u3002", name: "\u4e13\u5bb6\u540d\u79f0", skills: "\u64c5\u957f\u6280\u80fd", skillsHint: "\u591a\u4e2a\u6280\u80fd\u7528\u9017\u53f7\u5206\u9694", securityGroup: "\u5b89\u5168\u7ec4 ID", securityGroupHint: "\u540c\u5b89\u5168\u7ec4\u81ea\u7531\u8ba8\u8bba\u9700\u8981\u586b\u5199", modelVisibility: "\u6a21\u578b\u80fd\u529b\u5c55\u793a", hidden: "\u9690\u85cf", classOnly: "\u4ec5\u5c55\u793a\u80fd\u529b\u7c7b\u578b", providerAlias: "\u5c55\u793a\u63d0\u4f9b\u5546\u4e0e\u522b\u540d", description: "\u64c5\u957f\u63cf\u8ff0", invite: "\u9080\u8bf7\u4e0e\u9650\u5236", inviteDesc: "\u5b9a\u4e49\u9080\u8bf7\u5982\u4f55\u5904\u7406\uff0c\u4ee5\u53ca\u53ef\u5171\u4eab\u591a\u5c11\u4efb\u52a1\u4e0a\u4e0b\u6587\u3002", invitePolicy: "\u6536\u5230\u9080\u8bf7\u65f6", askAlways: "\u6bcf\u6b21\u8be2\u95ee", autoTrusted: "\u4fe1\u4efb\u5bf9\u8c61\u81ea\u52a8\u63a5\u53d7", observeOnly: "\u4ec5\u65c1\u542c\u81ea\u52a8\u63a5\u53d7", rejectAll: "\u5168\u90e8\u62d2\u7edd", sameSecurityGroup: "\u540c\u4e00\u5b89\u5168\u7ec4\u53ef\u81ea\u7531\u8ba8\u8bba", contextPolicy: "\u4e0a\u4e0b\u6587\u8303\u56f4", summaryOnly: "\u4ec5\u6458\u8981", summarySnippets: "\u6458\u8981\u548c\u5fc5\u8981\u7247\u6bb5", fullContext: "\u5b8c\u6574\u4e0a\u4e0b\u6587", maxRisk: "\u6700\u5927\u98ce\u9669", low: "\u4f4e", medium: "\u4e2d", high: "\u9ad8", maxRounds: "\u6700\u5927\u8f6e\u6570", timeout: "\u8d85\u65f6\u79d2\u6570", concurrent: "\u5e76\u53d1\u4e0a\u9650", note: "\u81ea\u7531\u8ba8\u8bba\u4ecd\u4ec5\u9650\u5f53\u524d Hub \u4e14\u540c\u4e00\u5b89\u5168\u7ec4\uff0c\u5e76\u53d7\u52ff\u6270\u3001\u98ce\u9669\u3001\u4e0a\u4e0b\u6587\u548c\u5e76\u53d1\u9650\u5236\u7ea6\u675f\u3002", roles: "\u5141\u8bb8\u53c2\u4e0e\u65b9\u5f0f", rolesDesc: "\u9009\u62e9\u6b64 MaClaw \u53c2\u4e0e\u7fa4\u7ec4\u8ba8\u8bba\u65f6\u5141\u8bb8\u6267\u884c\u7684\u884c\u4e3a\u3002", observe: "\u65c1\u542c", speak: "\u53d1\u8a00", propose: "\u63d0\u6848", review: "\u8bc4\u5ba1", vote: "\u6295\u7968", saved: "\u5df2\u4fdd\u5b58", enabledHint: "\u5df2\u51c6\u5907\u8fdb\u884c\u5f53\u524d Hub \u534f\u4f5c", disabledHint: "\u5df2\u4ece\u53d1\u73b0\u548c\u9080\u8bf7\u4e2d\u9690\u85cf" }
};
const zhHant = { ...copy.zhHans, title: "\u76ee\u524d Hub \u7fa4\u7d44\u8a0e\u8ad6", on: "\u5df2\u958b\u555f", off: "\u5df2\u95dc\u9589", securityGroup: "\u5b89\u5168\u7d44 ID", sameSecurityGroup: "\u540c\u4e00\u5b89\u5168\u7d44\u53ef\u81ea\u7531\u8a0e\u8ad6", saved: "\u5df2\u5132\u5b58" };
function normalize(config?: GroupDiscussionConfig): Required<GroupDiscussionConfig> { return { ...defaults, ...(config || {}), skills: Array.isArray(config?.skills) ? config.skills : defaults.skills, languages: Array.isArray(config?.languages) ? config.languages : defaults.languages, allowed_roles: Array.isArray(config?.allowed_roles) ? config.allowed_roles : defaults.allowed_roles }; }
function getExpertName(config: main.AppConfig | null, current: Required<GroupDiscussionConfig>): string { return ((config as any)?.maclaw_role_name || current.display_name || DEFAULT_NAME).trim(); }
function splitTags(value: string): string[] { return value.split(/[,;\uFF0C\uFF1B\n]/).map((item) => item.trim()).filter(Boolean); }

const panelStyle: CSSProperties = { display: "flex", flexDirection: "column", gap: "12px", maxWidth: "980px", margin: "0 auto", paddingBottom: "8px" };
const heroStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.xl, padding: "16px 18px", background: "linear-gradient(135deg, var(--theme-surface) 0%, var(--theme-surface-muted) 100%)", display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: "18px", alignItems: "center" };
const sectionStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.xl, padding: "14px", background: colors.surface };
const sectionHeaderStyle: CSSProperties = { display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "12px", marginBottom: "12px" };
const sectionTitleStyle: CSSProperties = { fontSize: "0.88rem", fontWeight: 800, color: colors.text, margin: 0, letterSpacing: "0.01em" };
const sectionDescStyle: CSSProperties = { fontSize: "0.72rem", color: colors.textMuted, lineHeight: 1.5, marginTop: "4px" };
const fieldStyle: CSSProperties = { border: "1px solid " + colors.border, borderRadius: radius.md, padding: "7px 9px", fontSize: "0.76rem", background: colors.bg, color: colors.text, width: "100%", boxSizing: "border-box" };
const labelStyle: CSSProperties = { fontSize: "0.72rem", color: colors.textSecondary, fontWeight: 700, marginBottom: "5px" };
const hintStyle: CSSProperties = { fontSize: "0.68rem", color: colors.textMuted, marginTop: "4px", lineHeight: 1.4 };
const gridStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(230px, 1fr))", gap: "12px 14px" };
const toggleListStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(250px, 1fr))", gap: "8px" };

function Section({ title, desc, children, aside }: { title: ReactNode; desc?: ReactNode; children: ReactNode; aside?: ReactNode }) {
  return <section style={sectionStyle}>
    <div style={sectionHeaderStyle}>
      <div><h4 style={sectionTitleStyle}>{title}</h4>{desc && <div style={sectionDescStyle}>{desc}</div>}</div>
      {aside}
    </div>
    {children}
  </section>;
}
function Field({ label, hint, children }: { label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return <div><div style={labelStyle}>{label}</div>{children}{hint && <div style={hintStyle}>{hint}</div>}</div>;
}
function ToggleRow({ checked, onChange, children }: { checked: boolean; onChange: (checked: boolean) => void; children: ReactNode }) {
  return <label style={{ display: "flex", alignItems: "center", gap: "9px", padding: "8px 10px", borderRadius: radius.md, border: "1px solid " + colors.borderLight, background: colors.bg, color: colors.text, fontSize: "0.76rem", fontWeight: 600, lineHeight: 1.35, cursor: "pointer" }}>
    <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} style={{ width: "15px", height: "15px", flexShrink: 0 }} />
    <span>{children}</span>
  </label>;
}
function StatusBadge({ enabled, text }: { enabled: boolean; text: string }) {
  return <span style={{ display: "inline-flex", alignItems: "center", gap: "6px", borderRadius: radius.pill, border: "1px solid " + (enabled ? colors.success : colors.border), background: enabled ? colors.successBg : colors.surfaceMuted, color: enabled ? colors.success : colors.textMuted, padding: "5px 10px", fontSize: "0.72rem", fontWeight: 800, whiteSpace: "nowrap" }}>
    <span style={{ width: "7px", height: "7px", borderRadius: "50%", background: "currentColor" }} />{text}
  </span>;
}

export function GroupDiscussionSettingsPanel({ config, saveRemoteConfigField, lang }: Props) {
  const c = lang === "zh-Hant" ? zhHant : lang === "zh-Hans" ? copy.zhHans : copy.en;
  const current = normalize((config as any)?.group_discussion);
  const expertName = getExpertName(config, current);
  const roleDescription = (config?.maclaw_role_description || DEFAULT_ROLE_DESCRIPTION).trim();
  const [skillsText, setSkillsText] = useState(current.skills.join(", "));
  const [expertNameDraft, setExpertNameDraft] = useState(expertName);
  const [roleDescriptionDraft, setRoleDescriptionDraft] = useState(roleDescription);
  const [saved, setSaved] = useState(false);
  const savedTimerRef = useRef<number | null>(null);
  const expertNameFocusedRef = useRef(false);
  const roleDescriptionFocusedRef = useRef(false);
  useEffect(() => setSkillsText(current.skills.join(", ")), [config]);
  useEffect(() => { if (!expertNameFocusedRef.current) setExpertNameDraft(expertName); }, [expertName]);
  useEffect(() => { if (!roleDescriptionFocusedRef.current) setRoleDescriptionDraft(roleDescription); }, [roleDescription]);
  useEffect(() => () => { if (savedTimerRef.current !== null) window.clearTimeout(savedTimerRef.current); }, []);
  const flashSaved = () => {
    setSaved(true);
    if (savedTimerRef.current !== null) window.clearTimeout(savedTimerRef.current);
    savedTimerRef.current = window.setTimeout(() => { setSaved(false); savedTimerRef.current = null; }, 1600);
  };
  const save = (patch: Partial<GroupDiscussionConfig>) => { saveRemoteConfigField({ group_discussion: normalize({ ...current, ...patch }) } as any); flashSaved(); };
  const saveExpertName = (value: string) => { const displayName = value.trim() || DEFAULT_NAME; setExpertNameDraft(displayName); saveRemoteConfigField({ maclaw_role_name: displayName, group_discussion: normalize({ ...current, display_name: displayName }) } as any); flashSaved(); };
  const saveRoleDescription = (value: string) => { const nextDescription = value.trim() || DEFAULT_ROLE_DESCRIPTION; setRoleDescriptionDraft(nextDescription); saveRemoteConfigField({ maclaw_role_description: nextDescription } as any); flashSaved(); };
  const saveGossipEnabled = (value: boolean) => { saveRemoteConfigField({ gossip_auto_publish: value } as any); flashSaved(); };
  const toggleRole = (role: string, checked: boolean) => { const roles = new Set(current.allowed_roles); if (checked) roles.add(role); else roles.delete(role); save({ allowed_roles: Array.from(roles) }); };
  const text = (en: string, zhHans: string, zhHant: string = zhHans) => lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en;

  return <div style={panelStyle}>
    <div style={heroStyle}>
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: "10px", flexWrap: "wrap", marginBottom: "8px" }}>
          <h3 style={{ margin: 0, color: colors.text, fontSize: "0.98rem", fontWeight: 900 }}>{c.title}</h3>
          <StatusBadge enabled={current.enabled} text={current.enabled ? c.on : c.off} />
          {saved && <span style={{ color: colors.success, fontSize: "0.72rem", fontWeight: 700 }}>{c.saved}</span>}
        </div>
        <div style={{ color: colors.textMuted, fontSize: "0.74rem", lineHeight: 1.55, maxWidth: "680px" }}>{c.intro}</div>
        <div style={{ color: current.enabled ? colors.success : colors.textMuted, fontSize: "0.72rem", fontWeight: 700, marginTop: "8px" }}>{current.enabled ? c.enabledHint : c.disabledHint}</div>
      </div>
      <label style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", justifySelf: "end", gap: "8px", border: "1px solid " + colors.border, borderRadius: radius.pill, padding: "8px 12px", background: colors.bg, color: colors.text, fontWeight: 800, fontSize: "0.76rem", cursor: "pointer", whiteSpace: "nowrap" }}>
        <input type="checkbox" checked={current.enabled} onChange={(e) => save({ enabled: e.target.checked })} style={{ width: "16px", height: "16px" }} />
        {current.enabled ? c.on : c.off}
      </label>
    </div>

    <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(320px, 1fr))", gap: "12px", alignItems: "start" }}>
      <Section title={c.basic} desc={c.basicDesc}>
        <div style={toggleListStyle}>
          <ToggleRow checked={current.discoverable} onChange={(checked) => save({ discoverable: checked })}>{c.listed}</ToggleRow>
          <ToggleRow checked={current.suggest_consultation} onChange={(checked) => save({ suggest_consultation: checked })}>{c.suggest}</ToggleRow>
          <ToggleRow checked={current.confirm_before_start} onChange={(checked) => save({ confirm_before_start: checked })}>{c.confirm}</ToggleRow>
          <ToggleRow checked={current.reject_when_dnd} onChange={(checked) => save({ reject_when_dnd: checked })}>{c.dnd}</ToggleRow>
        </div>
        <div style={{ marginTop: "12px" }}>
          <Field label={c.status}>
            <select style={fieldStyle} value={current.availability} onChange={(e) => save({ availability: e.target.value })}><option value="available">{c.available}</option><option value="busy">{c.busy}</option><option value="dnd">{c.dndStatus}</option></select>
          </Field>
        </div>
      </Section>

      <Section title={c.identity} desc={c.identityDesc}>
        <div style={gridStyle}>
          <Field label={c.name}><input style={fieldStyle} value={expertNameDraft} onFocus={() => { expertNameFocusedRef.current = true; }} onChange={(e) => setExpertNameDraft(e.target.value)} onBlur={() => { expertNameFocusedRef.current = false; saveExpertName(expertNameDraft); }} /></Field>
          <Field label={c.skills} hint={c.skillsHint}><input style={fieldStyle} value={skillsText} onChange={(e) => setSkillsText(e.target.value)} onBlur={() => save({ skills: splitTags(skillsText) })} /></Field>
          <Field label={c.securityGroup} hint={c.securityGroupHint}><input style={fieldStyle} value={current.security_group_id} placeholder="team-a" onChange={(e) => save({ security_group_id: e.target.value })} /></Field>
          <Field label={c.modelVisibility}><select style={fieldStyle} value={current.model_visibility} onChange={(e) => save({ model_visibility: e.target.value })}><option value="hidden">{c.hidden}</option><option value="class_only">{c.classOnly}</option><option value="provider_alias">{c.providerAlias}</option></select></Field>
        </div>
        <div style={{ marginTop: "12px", display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(280px, 1fr))", gap: "12px" }}>
          <Field label={text("Role Description", "角色描述", "角色描述")} hint={text("Used as MaClaw's local agent role prompt.", "作为本机 MaClaw Agent 的角色设定。", "作為本機 MaClaw Agent 的角色設定。")}><textarea style={{ ...fieldStyle, minHeight: 76, resize: "vertical" }} value={roleDescriptionDraft} onFocus={() => { roleDescriptionFocusedRef.current = true; }} onChange={(e) => setRoleDescriptionDraft(e.target.value)} onBlur={() => { roleDescriptionFocusedRef.current = false; saveRoleDescription(roleDescriptionDraft); }} /></Field>
          <Field label={c.description} hint={text("Shown to other experts in the current Hub.", "展示给当前 Hub 内其他专家看的擅长描述。", "展示給目前 Hub 內其他專家看的擅長描述。")}><textarea style={{ ...fieldStyle, minHeight: 76, resize: "vertical" }} value={current.description} onChange={(e) => save({ description: e.target.value })} /></Field>
        </div>
        <div style={{ marginTop: "12px" }}>
          <ToggleRow checked={(config as any)?.gossip_auto_publish !== false} onChange={saveGossipEnabled}>{text("Auto-post Chat Gossip", "聊天八卦自动发布", "聊天八卦自動發佈")}</ToggleRow>
        </div>
      </Section>
    </div>

    <Section title={c.invite} desc={c.inviteDesc}>
      <div style={gridStyle}>
        <Field label={c.invitePolicy}><select style={fieldStyle} value={current.invite_policy} onChange={(e) => save({ invite_policy: e.target.value })}><option value="ask_always">{c.askAlways}</option><option value="auto_trusted">{c.autoTrusted}</option><option value="observe_only_auto">{c.observeOnly}</option><option value="reject_all">{c.rejectAll}</option></select></Field>
        <Field label={c.contextPolicy}><select style={fieldStyle} value={current.context_policy} onChange={(e) => save({ context_policy: e.target.value })}><option value="summary_only">{c.summaryOnly}</option><option value="summary_snippets">{c.summarySnippets}</option><option value="full_context">{c.fullContext}</option></select></Field>
        <Field label={c.maxRisk}><select style={fieldStyle} value={current.max_risk_level} onChange={(e) => save({ max_risk_level: e.target.value })}><option value="low">{c.low}</option><option value="medium">{c.medium}</option><option value="high">{c.high}</option></select></Field>
        <Field label={c.maxRounds}><input type="number" style={fieldStyle} min={1} max={20} value={current.max_rounds} onChange={(e) => save({ max_rounds: Number(e.target.value || 1) })} /></Field>
        <Field label={c.timeout}><input type="number" style={fieldStyle} min={30} max={3600} value={current.timeout_seconds} onChange={(e) => save({ timeout_seconds: Number(e.target.value || 300) })} /></Field>
        <Field label={c.concurrent}><input type="number" style={fieldStyle} min={1} max={8} value={current.concurrent_limit} onChange={(e) => save({ concurrent_limit: Number(e.target.value || 1) })} /></Field>
      </div>
      <div style={{ marginTop: "12px", display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: "12px", alignItems: "stretch" }}>
        <ToggleRow checked={current.allow_security_group_free_discussion} onChange={(checked) => save({ allow_security_group_free_discussion: checked })}>{c.sameSecurityGroup}</ToggleRow>
        <div style={{ border: "1px solid " + colors.borderLight, borderRadius: radius.md, background: colors.bg, padding: "8px 10px", color: colors.textMuted, fontSize: "0.72rem", lineHeight: 1.5 }}>{c.note}</div>
      </div>
    </Section>

    <Section title={c.roles} desc={c.rolesDesc}>
      <div style={{ display: "flex", flexWrap: "wrap", gap: "8px" }}>
        {[["observe", c.observe], ["speak", c.speak], ["propose", c.propose], ["review", c.review], ["vote", c.vote]].map(([role, text]) => <ToggleRow key={role} checked={current.allowed_roles.includes(role)} onChange={(checked) => toggleRole(role, checked)}>{text}</ToggleRow>)}
      </div>
    </Section>
  </div>;
}
