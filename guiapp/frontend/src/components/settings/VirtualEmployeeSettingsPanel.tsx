import { useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { EventsOn, EventsOff, BrowserOpenURL } from "../../../wailsjs/runtime";
import { localizeText } from "../../i18n";
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
  SetAuthRequestSoundConfig,
} from "../../../wailsjs/go/main/App";
import { avatarImageMaxBytes, avatarSourceImageMaxBytes, safeAvatarDataURL, safeAvatarSourceDataURL } from "../ai/virtualEmployeeAvatar";
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
    avatar_data_url?: string;
  };
};

export type AccessPolicy = "public" | "whitelist" | "blacklist" | "per_request";
export type VEStatus = "pending" | "active" | "disabled" | "rejected";
type SensitiveQueryPolicy = "confirm" | "deny" | "allow";
type AuthRequestSoundPreset = "classic" | "soft" | "bright" | "pulse" | "urgent";

type VEFormStateSetter = {
  setRegistered: (value: boolean) => void;
  setStatus: (value: VEStatus | null) => void;
  setName: (value: string) => void;
  setSkillDescription: (value: string) => void;
  setAccessPolicy: (value: AccessPolicy | "") => void;
  setWhitelist: (value: string[]) => void;
  setBlacklist: (value: string[]) => void;
  setListInput: (value: string) => void;
  setAvatarDataURL: (value: string) => void;
  setAvatarSourceURL: (value: string) => void;
  setAvatarScale: (value: number) => void;
  setAvatarOffsetX: (value: number) => void;
  setAvatarOffsetY: (value: number) => void;
  setAvatarNeedsCrop: (value: boolean) => void;
  setAvatarImageSize: (value: { width: number; height: number } | null) => void;
  setAvatarError: (value: string) => void;
  setNameError: (value: string) => void;
  setSkillError: (value: string) => void;
  setPolicyError: (value: string) => void;
};

const avatarMaxFileSizeMB = avatarSourceImageMaxBytes / (1024 * 1024);
const avatarSavedMaxSizeMB = avatarImageMaxBytes / (1024 * 1024);
const authRequestSoundOptions = (lang?: string): { value: AuthRequestSoundPreset; label: string }[] => [
  { value: "classic", label: textForLang(lang, "Classic phone", "经典电话铃", "經典電話鈴") },
  { value: "soft", label: textForLang(lang, "Soft chime", "柔和铃音", "柔和鈴音") },
  { value: "bright", label: textForLang(lang, "Bright desk phone", "清亮座机", "清亮座機") },
  { value: "pulse", label: textForLang(lang, "Pulse alert", "脉冲提醒", "脈衝提醒") },
  { value: "urgent", label: textForLang(lang, "Urgent ring", "急促铃声", "急促鈴聲") },
];

function normalizeAuthRequestSoundPreset(value: unknown): AuthRequestSoundPreset {
  const preset = String(value || "").trim().toLowerCase() as AuthRequestSoundPreset;
  return ["classic", "soft", "bright", "pulse", "urgent"].includes(preset) ? preset : "classic";
}

function resetVEFormState(setter: VEFormStateSetter) {
  setter.setRegistered(false);
  setter.setStatus(null);
  setter.setName("");
  setter.setSkillDescription("");
  setter.setAccessPolicy("");
  setter.setWhitelist([]);
  setter.setBlacklist([]);
  setter.setListInput("");
  setter.setAvatarDataURL("");
  setter.setAvatarSourceURL("");
  setter.setAvatarScale(1);
  setter.setAvatarOffsetX(0);
  setter.setAvatarOffsetY(0);
  setter.setAvatarNeedsCrop(false);
  setter.setAvatarImageSize(null);
  setter.setAvatarError("");
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
) => localizeText(lang, en, zhHans, zhHant);


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
      "First access requires confirmation",
      "首次访问需确认",
      "首次訪問需確認",
    ),
  },
];

function accessListKey(value: string): string {
  return value.trim().toLowerCase();
}

function appendAccessListItem(values: string[], value: string): string[] {
  const item = value.trim();
  if (!item) return values;
  const key = accessListKey(item);
  if (values.some((existing) => accessListKey(existing) === key)) return values;
  return [...values, item];
}

function removeAccessListItem(values: string[], value: string): string[] {
  const key = accessListKey(value);
  return values.filter((existing) => accessListKey(existing) !== key);
}

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
  avatar: textForLang(lang, "Employee photo", "\u6570\u5b57\u5458\u5de5\u5f62\u8c61", "\u6578\u5b57\u54e1\u5de5\u5f62\u8c61"),
  avatarUpload: textForLang(lang, "Upload photo", "\u4e0a\u4f20\u7167\u7247", "\u4e0a\u50b3\u7167\u7247"),
  avatarReplace: textForLang(lang, "Replace", "\u66ff\u6362", "\u66ff\u63db"),
  avatarClear: textForLang(lang, "Use default", "\u4f7f\u7528\u9ed8\u8ba4", "\u4f7f\u7528\u9810\u8a2d"),
  avatarPreparing: textForLang(lang, "Processing image...", "\u5904\u7406\u56fe\u7247\u4e2d...", "\u8655\u7406\u5716\u7247\u4e2d..."),
  avatarZoom: textForLang(lang, "Zoom", "\u7f29\u653e", "\u7e2e\u653e"),
  avatarHorizontal: textForLang(lang, "Horizontal", "\u6c34\u5e73", "\u6c34\u5e73"),
  avatarVertical: textForLang(lang, "Vertical", "\u5782\u76f4", "\u5782\u76f4"),
  avatarCircle: textForLang(lang, "Circular display", "\u5706\u5f62\u663e\u793a", "\u5713\u5f62\u986f\u793a"),
  avatarHint: textForLang(
    lang,
    `Supports PNG, JPG/JPEG, and WebP up to ${avatarMaxFileSizeMB} MB. Large photos are resized locally before preview. The cropped avatar is saved as JPEG up to ${avatarSavedMaxSizeMB} MB. Preview is what other users will see. Leave empty to keep the current default display.`,
    `\u652f\u6301 PNG\u3001JPG/JPEG\u3001WebP\uff0c\u539f\u56fe\u6700\u5927 ${avatarMaxFileSizeMB} MB\u3002\u5927\u56fe\u4f1a\u5148\u5728\u672c\u5730\u7f29\u5c0f\u518d\u9884\u89c8\u3002\u88c1\u526a\u540e\u5934\u50cf\u4fdd\u5b58\u4e3a JPEG\uff0c\u6700\u5927 ${avatarSavedMaxSizeMB} MB\u3002\u9884\u89c8\u5373\u5176\u4ed6\u7528\u6237\u770b\u5230\u7684\u6548\u679c\u3002\u4e0d\u8bbe\u7f6e\u5219\u4fdd\u6301\u73b0\u6709\u9ed8\u8ba4\u663e\u793a\u3002`,
    `\u652f\u63f4 PNG\u3001JPG/JPEG\u3001WebP\uff0c\u539f\u5716\u6700\u5927 ${avatarMaxFileSizeMB} MB\u3002\u5927\u5716\u6703\u5148\u5728\u672c\u5730\u7e2e\u5c0f\u518d\u9810\u89bd\u3002\u88c1\u526a\u5f8c\u982d\u50cf\u5132\u5b58\u70ba JPEG\uff0c\u6700\u5927 ${avatarSavedMaxSizeMB} MB\u3002\u9810\u89bd\u5373\u5176\u4ed6\u7528\u6236\u770b\u5230\u7684\u6548\u679c\u3002\u4e0d\u8a2d\u5b9a\u5247\u4fdd\u6301\u73fe\u6709\u9810\u8a2d\u986f\u793a\u3002`,
  ),
  avatarInvalid: textForLang(
    lang,
    `Please upload a PNG, JPG/JPEG, or WebP image up to ${avatarMaxFileSizeMB} MB.`,
    `\u8bf7\u4e0a\u4f20 PNG\u3001JPG/JPEG \u6216 WebP \u56fe\u7247\uff0c\u5927\u5c0f\u4e0d\u8d85\u8fc7 ${avatarMaxFileSizeMB} MB\u3002`,
    `\u8acb\u4e0a\u50b3 PNG\u3001JPG/JPEG \u6216 WebP \u5716\u7247\uff0c\u5927\u5c0f\u4e0d\u8d85\u904e ${avatarMaxFileSizeMB} MB\u3002`,
  ),
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

const avatarCanvasSize = 256;
const avatarPreviewSize = 112;
const avatarMaxFileSize = avatarSourceImageMaxBytes;
const avatarSourceMaxDimension = 1024;
const avatarSourceJPEGQuality = 0.9;
const avatarAcceptedMimeTypes = new Set(["image/png", "image/jpeg", "image/webp"]);
const avatarAcceptAttr = "image/png,image/jpeg,image/webp";

function calculateAvatarDrawFrame(width: number, height: number, size: number, scale: number, offsetX: number, offsetY: number) {
  const base = Math.max(size / width, size / height);
  const drawW = width * base * scale;
  const drawH = height * base * scale;
  const maxShiftX = Math.max(0, (drawW - size) / 2);
  const maxShiftY = Math.max(0, (drawH - size) / 2);
  return {
    drawW,
    drawH,
    dx: (size - drawW) / 2 + (offsetX / 100) * maxShiftX,
    dy: (size - drawH) / 2 + (offsetY / 100) * maxShiftY,
  };
}

function buildCroppedAvatarDataURL(sourceURL: string, scale: number, offsetX: number, offsetY: number): Promise<string> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => {
      const canvas = document.createElement("canvas");
      canvas.width = avatarCanvasSize;
      canvas.height = avatarCanvasSize;
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        reject(new Error("canvas unavailable"));
        return;
      }
      ctx.clearRect(0, 0, avatarCanvasSize, avatarCanvasSize);
      const { drawW, drawH, dx, dy } = calculateAvatarDrawFrame(img.width, img.height, avatarCanvasSize, scale, offsetX, offsetY);
      ctx.drawImage(img, dx, dy, drawW, drawH);
      resolve(canvas.toDataURL("image/jpeg", 0.86));
    };
    img.onerror = () => reject(new Error("image load failed"));
    img.src = sourceURL;
  });
}

function prepareAvatarSourceDataURL(sourceURL: string, sourceBytes: number): Promise<string> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => {
      const width = img.width || avatarSourceMaxDimension;
      const height = img.height || avatarSourceMaxDimension;
      const ratio = Math.min(1, avatarSourceMaxDimension / Math.max(width, height));
      if (ratio === 1 && sourceBytes <= avatarImageMaxBytes) {
        resolve(sourceURL);
        return;
      }
      const targetW = Math.max(1, Math.round(width * ratio));
      const targetH = Math.max(1, Math.round(height * ratio));
      const canvas = document.createElement("canvas");
      canvas.width = targetW;
      canvas.height = targetH;
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        reject(new Error("canvas unavailable"));
        return;
      }
      ctx.drawImage(img, 0, 0, targetW, targetH);
      resolve(canvas.toDataURL("image/jpeg", avatarSourceJPEGQuality));
    };
    img.onerror = () => reject(new Error("image load failed"));
    img.src = sourceURL;
  });
}

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
  const sectionId = useId();
  const [name, setName] = useState("");
  const [skillDescription, setSkillDescription] = useState("");
  const [accessPolicy, setAccessPolicy] = useState<AccessPolicy | "">("");
  const [whitelist, setWhitelist] = useState<string[]>([]);
  const [blacklist, setBlacklist] = useState<string[]>([]);
  const [status, setStatus] = useState<VEStatus | null>(null);
  const [registered, setRegistered] = useState(false);
  const [listInput, setListInput] = useState("");
  const [avatarDataURL, setAvatarDataURL] = useState("");
  const [avatarSourceURL, setAvatarSourceURL] = useState("");
  const [avatarScale, setAvatarScale] = useState(1);
  const [avatarOffsetX, setAvatarOffsetX] = useState(0);
  const [avatarOffsetY, setAvatarOffsetY] = useState(0);
  const [avatarNeedsCrop, setAvatarNeedsCrop] = useState(false);
  const [avatarCircle, setAvatarCircle] = useState(true);
  const [avatarImageSize, setAvatarImageSize] = useState<{ width: number; height: number } | null>(null);
  const [avatarError, setAvatarError] = useState("");
  const [avatarPreparing, setAvatarPreparing] = useState(false);
  const avatarFileInputRef = useRef<HTMLInputElement | null>(null);
  const avatarFileLoadSeqRef = useRef(0);
  const [approvalNotice, setApprovalNotice] = useState("");
  const [sensitiveQueryPolicy, setSensitiveQueryPolicy] =
    useState<SensitiveQueryPolicy>("confirm");
  const [authRequestSoundPreset, setAuthRequestSoundPreset] = useState<AuthRequestSoundPreset>("classic");
  const [authRequestSoundMuted, setAuthRequestSoundMuted] = useState(false);
  const authRequestSoundSaveSeqRef = useRef(0);
  const [nameError, setNameError] = useState("");
  const [skillError, setSkillError] = useState("");
  const [policyError, setPolicyError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const mountedRef = useRef(true);

  // --- Allowed Directories state ---
  const [allowedDirs, setAllowedDirs] = useState<string[]>([]);
  const [dirDuplicateWarning, setDirDuplicateWarning] = useState("");

  async function loadStatus(avatarDataURLFallback = "") {
    try {
      const resp = (await GetVEStatus()) as VEStatusResponse;
      if (!mountedRef.current) return;
      avatarFileLoadSeqRef.current += 1;
      setAvatarPreparing(false);
      if (resp?.registered && resp.employee) {
        setRegistered(true);
        setName(resp.employee.name || "");
        setSkillDescription(resp.employee.skill_description || "");
        setAccessPolicy((resp.employee.access_policy as AccessPolicy) || "");
        setWhitelist(Array.isArray(resp.employee.whitelist) ? resp.employee.whitelist : []);
        setBlacklist(Array.isArray(resp.employee.blacklist) ? resp.employee.blacklist : []);
        const avatarDataURL = safeAvatarDataURL(resp.employee.avatar_data_url) || safeAvatarDataURL(avatarDataURLFallback);
        setAvatarDataURL(avatarDataURL);
        setAvatarSourceURL(avatarDataURL);
        setAvatarScale(1);
        setAvatarOffsetX(0);
        setAvatarOffsetY(0);
        setAvatarNeedsCrop(false);
        setAvatarImageSize(null);
        setAvatarError("");
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
        setAvatarDataURL,
        setAvatarSourceURL,
        setAvatarScale,
        setAvatarOffsetX,
        setAvatarOffsetY,
        setAvatarNeedsCrop,
        setAvatarImageSize,
        setAvatarError,
        setNameError,
        setSkillError,
        setPolicyError,
      });
      }
    } catch {
      if (!mountedRef.current) return;
      avatarFileLoadSeqRef.current += 1;
      setAvatarPreparing(false);
      resetVEFormState({
          setRegistered,
          setStatus,
          setName,
          setSkillDescription,
          setAccessPolicy,
          setWhitelist,
          setBlacklist,
          setListInput,
          setAvatarDataURL,
          setAvatarSourceURL,
          setAvatarScale,
          setAvatarOffsetX,
          setAvatarOffsetY,
          setAvatarNeedsCrop,
          setAvatarImageSize,
          setAvatarError,
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

  useEffect(() => {
    let cancelled = false;
    LoadConfig()
      .then((cfg: any) => {
        if (cancelled) return;
        const gd = cfg?.group_discussion || cfg?.GroupDiscussion || {};
        setAuthRequestSoundPreset(normalizeAuthRequestSoundPreset(gd.auth_request_sound_preset ?? gd.AuthRequestSoundPreset));
        setAuthRequestSoundMuted(Boolean(gd.auth_request_sound_muted ?? gd.AuthRequestSoundMuted));
      })
      .catch(() => {
        if (!cancelled) {
          setAuthRequestSoundPreset("classic");
          setAuthRequestSoundMuted(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function handleAvatarFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const loadSeq = ++avatarFileLoadSeqRef.current;
    if (!avatarAcceptedMimeTypes.has(file.type) || file.size > avatarMaxFileSize) {
      setAvatarPreparing(false);
      setAvatarError(c.avatarInvalid);
      e.target.value = "";
      return;
    }
    setAvatarPreparing(true);
    setAvatarError("");
    const reader = new FileReader();
    reader.onload = () => {
      if (!mountedRef.current || loadSeq !== avatarFileLoadSeqRef.current) return;
      const dataURL = String(reader.result || "");
      if (!safeAvatarSourceDataURL(dataURL)) {
        setAvatarPreparing(false);
        setAvatarError(c.avatarInvalid);
        return;
      }
      prepareAvatarSourceDataURL(dataURL, file.size)
        .then((preparedURL) => {
          if (!mountedRef.current || loadSeq !== avatarFileLoadSeqRef.current) return;
          if (!safeAvatarSourceDataURL(preparedURL)) {
            setAvatarPreparing(false);
            setAvatarError(c.avatarInvalid);
            return;
          }
          setAvatarSourceURL(preparedURL);
          setAvatarScale(1);
          setAvatarOffsetX(0);
          setAvatarOffsetY(0);
          setAvatarNeedsCrop(true);
          setAvatarImageSize(null);
          setAvatarError("");
          setAvatarPreparing(false);
        })
        .catch(() => {
          if (mountedRef.current && loadSeq === avatarFileLoadSeqRef.current) {
            setAvatarPreparing(false);
            setAvatarError(c.avatarInvalid);
          }
        });
    };
    reader.onerror = () => {
      if (mountedRef.current && loadSeq === avatarFileLoadSeqRef.current) {
        setAvatarPreparing(false);
        setAvatarError(c.avatarInvalid);
      }
    };
    reader.readAsDataURL(file);
    e.target.value = "";
  }

  function handleClearAvatar() {
    avatarFileLoadSeqRef.current += 1;
    setAvatarPreparing(false);
    setAvatarSourceURL("");
    setAvatarDataURL("");
    setAvatarScale(1);
    setAvatarOffsetX(0);
    setAvatarOffsetY(0);
    setAvatarNeedsCrop(false);
    setAvatarImageSize(null);
    setAvatarError("");
  }

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
            `\u76ee\u5f55 "${selected}" \u5df2\u5728\u5217\u8868\u4e2d\u3002`,
            `\u76ee\u9304 "${selected}" \u5df2\u5728\u5217\u8868\u4e2d\u3002`,
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
      // Persist failed 闁?keep UI unchanged
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
  const avatarPreviewURL = avatarSourceURL || avatarDataURL;
  let avatarPreviewStyle: CSSProperties | undefined;
  if (avatarSourceURL && avatarImageSize) {
    const { drawW, drawH, dx, dy } = calculateAvatarDrawFrame(
      avatarImageSize.width,
      avatarImageSize.height,
      avatarPreviewSize,
      avatarScale,
      avatarOffsetX,
      avatarOffsetY,
    );
    avatarPreviewStyle = {
      width: `${drawW}px`,
      height: `${drawH}px`,
      left: `${dx}px`,
      top: `${dy}px`,
      transform: "none",
    };
  }

  async function saveAuthRequestSound(next: { preset?: AuthRequestSoundPreset; muted?: boolean }) {
    const previousPreset = authRequestSoundPreset;
    const previousMuted = authRequestSoundMuted;
    const preset = next.preset ?? previousPreset;
    const muted = next.muted ?? previousMuted;
    const saveSeq = ++authRequestSoundSaveSeqRef.current;
    setAuthRequestSoundPreset(preset);
    setAuthRequestSoundMuted(muted);
    try {
      await SetAuthRequestSoundConfig(preset, muted);
    } catch {
      if (authRequestSoundSaveSeqRef.current === saveSeq) {
        setAuthRequestSoundPreset(previousPreset);
        setAuthRequestSoundMuted(previousMuted);
      }
    }
  }

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
    if (avatarPreparing) return;
    if (!validate()) return;
    setSubmitting(true);
    let finalAvatarDataURL = avatarDataURL;
    let croppedAvatarDataURL = "";
    if (avatarSourceURL && avatarNeedsCrop) {
      try {
        finalAvatarDataURL = await buildCroppedAvatarDataURL(avatarSourceURL, avatarScale, avatarOffsetX, avatarOffsetY);
        croppedAvatarDataURL = finalAvatarDataURL;
      } catch {
        setAvatarError(c.avatarInvalid);
        if (mountedRef.current) setSubmitting(false);
        return;
      }
    }
    const list =
      accessPolicy === "whitelist"
        ? whitelist
        : accessPolicy === "blacklist"
          ? blacklist
          : [];
    try {
      if (registered && !isRejected) {
        await UpdateVESettings(name, skillDescription, accessPolicy, list, finalAvatarDataURL);
      } else {
        await RegisterVirtualEmployee(
          name,
          skillDescription,
          accessPolicy,
          list,
          finalAvatarDataURL,
        );
        setRegistered(true);
        setStatus("pending");
      }
      if (croppedAvatarDataURL && mountedRef.current) {
        setAvatarDataURL(croppedAvatarDataURL);
        setAvatarSourceURL(croppedAvatarDataURL);
        setAvatarScale(1);
        setAvatarOffsetX(0);
        setAvatarOffsetY(0);
        setAvatarImageSize(null);
        setAvatarNeedsCrop(false);
      }
      await loadStatus(finalAvatarDataURL);
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
    if (accessPolicy === "whitelist")
      setWhitelist((prev) => appendAccessListItem(prev, item));
    if (accessPolicy === "blacklist")
      setBlacklist((prev) => appendAccessListItem(prev, item));
    setListInput("");
  }

  function handleRemoveFromList(item: string) {
    if (accessPolicy === "whitelist")
      setWhitelist((prev) => removeAccessListItem(prev, item));
    if (accessPolicy === "blacklist")
      setBlacklist((prev) => removeAccessListItem(prev, item));
  }

  const showListEditor =
    accessPolicy === "whitelist" || accessPolicy === "blacklist";
  const approvalWorkflowDesign = (
    <div data-testid="ve-approval-workflow-design-section" className="ve-form-group ve-form-group--nested">
      <div className="ve-form-row">
        <label className="ve-form-label">
          {textForLang(lang, "Approval Workflow", "\u5ba1\u6279\u5de5\u4f5c\u6d41", "\u5be9\u6279\u5de5\u4f5c\u6d41")}
        </label>
        <div className="ve-form-field">
          <button
            type="button"
            className="ve-btn ve-btn--secondary"
            data-testid="ve-approval-workflow-design-btn"
            onClick={async () => {
              try {
                const cfg = await LoadConfig() as {
                  remote_hub_url?: string;
                  remote_machine_id?: string;
                  remote_machine_token?: string;
                } | null;
                const hubUrl = String(cfg?.remote_hub_url || "").trim().replace(/\/+$/, "");
                if (hubUrl) {
                  const machineId = String(cfg?.remote_machine_id || remoteMachineId || "").trim();
                  const machineToken = String(cfg?.remote_machine_token || "").trim();
                  const auth = new URLSearchParams();
                  if (machineId) auth.set("machine_id", machineId);
                  if (machineToken) auth.set("token", machineToken);
                  const authFragment = auth.toString();
                  BrowserOpenURL(`${hubUrl}/approval_workflow${authFragment ? `#${authFragment}` : ""}`);
                }
              } catch {
                // Config load failed; keep the settings panel responsive.
              }
            }}
          >
            {textForLang(lang, "Approval Workflow Design", "\u5ba1\u6279\u5de5\u4f5c\u6d41\u8bbe\u8ba1", "\u5be9\u6279\u5de5\u4f5c\u6d41\u8a2d\u8a08")}
          </button>
          <p className="ve-form-hint">
            {textForLang(
              lang,
              "Open the visual workflow designer on Hub to create and edit approval workflows.",
              "\u6253\u5f00 Hub \u4e0a\u7684\u53ef\u89c6\u5316\u5de5\u4f5c\u6d41\u8bbe\u8ba1\u5668\uff0c\u521b\u5efa\u548c\u7f16\u8f91\u5ba1\u6279\u5de5\u4f5c\u6d41\u3002",
              "\u958b\u555f Hub \u4e0a\u7684\u8996\u89ba\u5316\u5de5\u4f5c\u6d41\u8a2d\u8a08\u5668\uff0c\u5efa\u7acb\u548c\u7de8\u8f2f\u5be9\u6279\u5de5\u4f5c\u6d41\u3002",
            )}
          </p>
        </div>
      </div>
    </div>
  );

  return (
    <div data-testid="ve-settings-panel" className="ve-settings-panel">
      <div className="ve-settings-header">
        <h3 className="ve-settings-title">{c.title}</h3>
        {status && (
          <span
            data-testid="ve-status-badge"
            className="ve-status-badge"
            data-status={status}
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
        <section className="ve-form-section" aria-labelledby={`${sectionId}-identity`}>
          <div className="ve-form-section__header">
            <h4 className="ve-form-section__title" id={`${sectionId}-identity`}>
              {textForLang(lang, "Identity", "\u8eab\u4efd\u4fe1\u606f", "\u8eab\u5206\u8cc7\u8a0a")}
            </h4>
          </div>

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
            <label className="ve-form-label">{c.avatar}</label>
            <div className="ve-form-field">
              <div className="ve-avatar-editor" data-testid="ve-avatar-editor">
                <div className="ve-avatar-editor__stage" data-circle={avatarCircle ? "true" : "false"}>
                  {avatarPreviewURL ? (
                    <img
                      src={avatarPreviewURL}
                      alt=""
                      className="ve-avatar-editor__image ve-avatar-editor__image--fit"
                      style={avatarPreviewStyle}
                      onLoad={(e) => {
                        const img = e.currentTarget;
                        if (img.naturalWidth > 0 && img.naturalHeight > 0) {
                          setAvatarImageSize({ width: img.naturalWidth, height: img.naturalHeight });
                        }
                      }}
                      onError={(e) => {
                        const failedSource = e.currentTarget.currentSrc || e.currentTarget.src;
                        const replacementFailed = !!avatarSourceURL && avatarSourceURL !== avatarDataURL && failedSource === avatarSourceURL;
                        setAvatarSourceURL("");
                        if (!replacementFailed) setAvatarDataURL("");
                        setAvatarImageSize(null);
                        setAvatarNeedsCrop(false);
                        setAvatarError(c.avatarInvalid);
                      }}
                    />
                  ) : (
                    <span className="ve-avatar-editor__fallback" aria-hidden="true">
                      {name.trim().slice(0, 1).toUpperCase() || "DE"}
                    </span>
                  )}
                </div>
                <div className="ve-avatar-editor__controls">
                  <div className="ve-avatar-editor__actions">
                    <input
                      ref={avatarFileInputRef}
                      data-testid="ve-avatar-file-input"
                      type="file"
                      accept={avatarAcceptAttr}
                      className="ve-avatar-editor__file"
                      onChange={handleAvatarFileChange}
                      disabled={formDisabled}
                    />
                    <button
                      type="button"
                      className="ve-btn ve-btn--secondary"
                      onClick={() => avatarFileInputRef.current?.click()}
                      disabled={formDisabled}
                      data-testid="ve-avatar-upload-btn"
                    >
                      {avatarPreviewURL ? c.avatarReplace : c.avatarUpload}
                    </button>
                    {avatarPreviewURL && !formDisabled && (
                      <button
                        type="button"
                        className="ve-btn ve-btn--ghost"
                        onClick={handleClearAvatar}
                        data-testid="ve-avatar-clear-btn"
                      >
                        {c.avatarClear}
                      </button>
                    )}
                  </div>
                  <label className="ve-avatar-editor__switch">
                    <input
                      type="checkbox"
                      checked={avatarCircle}
                      onChange={(e) => setAvatarCircle(e.target.checked)}
                    />
                    <span>{c.avatarCircle}</span>
                  </label>
                  {avatarSourceURL && !formDisabled && (
                    <div className="ve-avatar-editor__sliders">
                      <label>
                        <span>{c.avatarZoom}</span>
                        <input type="range" min="1" max="3" step="0.01" value={avatarScale} onChange={(e) => { setAvatarScale(Number(e.target.value)); setAvatarNeedsCrop(true); }} />
                      </label>
                      <label>
                        <span>{c.avatarHorizontal}</span>
                        <input type="range" min="-80" max="80" step="1" value={avatarOffsetX} onChange={(e) => { setAvatarOffsetX(Number(e.target.value)); setAvatarNeedsCrop(true); }} />
                      </label>
                      <label>
                        <span>{c.avatarVertical}</span>
                        <input type="range" min="-80" max="80" step="1" value={avatarOffsetY} onChange={(e) => { setAvatarOffsetY(Number(e.target.value)); setAvatarNeedsCrop(true); }} />
                      </label>
                    </div>
                  )}
                  <p className="ve-form-hint">{c.avatarHint}</p>
                  {avatarError && <span role="alert" className="ve-form-error">{avatarError}</span>}
                </div>
              </div>
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
        </section>

        <section className="ve-form-section" aria-labelledby={`${sectionId}-access`}>
          <div className="ve-form-section__header">
            <h4 className="ve-form-section__title" id={`${sectionId}-access`}>
              {textForLang(lang, "Access & Requests", "\u8bbf\u95ee\u4e0e\u8bf7\u6c42", "\u5b58\u53d6\u8207\u8acb\u6c42")}
            </h4>
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

          {showListEditor && !formDisabled && (
            <div data-testid="list-editor" className="ve-form-group">
              <label className="ve-form-label" htmlFor={`${sectionId}-access-list-input`}>
                {accessPolicy === "whitelist" ? c.whitelist : c.blacklist}
              </label>
              <div className="ve-list-input-row">
                <input
                  id={`${sectionId}-access-list-input`}
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

          <div data-testid="ve-auth-sound-section" className="ve-form-group">
          <div className="ve-form-row-dual">
            <div className="ve-form-row-dual-item">
              <label className="ve-form-label" htmlFor="ve-auth-sound-preset">
                {textForLang(lang, "Access request ringtone", "访问请求铃声", "存取請求鈴聲")}
              </label>
              <select
                id="ve-auth-sound-preset"
                className="ve-form-input ve-form-select"
                value={authRequestSoundPreset}
                disabled={authRequestSoundMuted}
                onChange={(e) => saveAuthRequestSound({ preset: normalizeAuthRequestSoundPreset(e.target.value) })}
              >
                {authRequestSoundOptions(lang).map((opt) => (
                  <option key={opt.value} value={opt.value}>{opt.label}</option>
                ))}
              </select>
            </div>
            <div className="ve-form-row-dual-item">
              <label className="ve-form-label" htmlFor="ve-auth-sound-muted">
                {textForLang(lang, "Ringtone mode", "铃声模式", "鈴聲模式")}
              </label>
              <label className="ve-avatar-editor__switch" style={{ minHeight: 34, alignItems: "center" }}>
                <input
                  id="ve-auth-sound-muted"
                  type="checkbox"
                  checked={!authRequestSoundMuted}
                  onChange={(e) => saveAuthRequestSound({ muted: !e.target.checked })}
                />
                <span>{textForLang(lang, "Play sound on access request", "收到访问请求时播放铃声", "收到存取請求時播放鈴聲")}</span>
              </label>
            </div>
          </div>
          <p className="ve-form-hint">
            {textForLang(
              lang,
              "When another user requests access to this GUI digital employee, the top reminder will blink and play this ringtone for several seconds.",
              "当别人请求访问这个 GUI 数字员工时，顶部提醒会闪烁，并播放这段铃声数秒。",
              "當別人請求存取這個 GUI 數字員工時，頂部提醒會閃爍，並播放這段鈴聲數秒。",
            )}
          </p>
          </div>
        </section>

        {/* Allowed access directory section (Requirements 1.1-1.8, 2.2) */}
        <section data-testid="ve-allowed-dirs-section" className="ve-form-section ve-form-section--compact" aria-labelledby={`${sectionId}-files`}>
          <div className="ve-form-section__header">
            <h4 className="ve-form-section__title" id={`${sectionId}-files`}>
              {textForLang(lang, "File Access", "\u6587\u4ef6\u8bbf\u95ee", "\u6a94\u6848\u5b58\u53d6")}
            </h4>
          </div>
          <div className="ve-dirs-header">
            <label className="ve-form-label">
              {textForLang(lang, "Allowed Access Directories", "\u5141\u8bb8\u8bbf\u95ee\u76ee\u5f55", "\u5141\u8a31\u5b58\u53d6\u76ee\u9304")}
            </label>
            <button
              className="ve-btn ve-btn--link"
              onClick={handleAddDirectory}
              data-testid="ve-add-dir-btn"
            >
              {textForLang(lang, "Add Directory", "\u6dfb\u52a0\u76ee\u5f55", "\u65b0\u589e\u76ee\u9304")}
            </button>
          </div>
          {dirDuplicateWarning && (
            <div
              className="ve-notice ve-notice--warning ve-notice--compact"
              role="alert"
              data-testid="ve-dir-duplicate-warning"
            >
              {dirDuplicateWarning}
            </div>
          )}
          {allowedDirs.length > 0 && (
            <ul data-testid="ve-allowed-dirs-list" className="ve-list-items">
              {allowedDirs.map((dir) => (
                <li key={dir} className="ve-list-item">
                  <span className="ve-list-item-text" title={dir}>{dir}</span>
                  <button
                    className="ve-btn ve-btn--ghost"
                    onClick={() => handleRemoveDirectory(dir)}
                    data-testid={`ve-remove-dir-${dir}`}
                    aria-label={`Remove ${dir}`}
                  >
                    {c.remove}
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
                "\u672a\u914d\u7f6e\u76ee\u5f55\u3002\u6570\u5b57\u5458\u5de5\u5728\u6dfb\u52a0\u81f3\u5c11\u4e00\u4e2a\u76ee\u5f55\u524d\u65e0\u6cd5\u53d1\u9001\u6587\u4ef6\u3002",
                "\u672a\u8a2d\u5b9a\u76ee\u9304\u3002\u6578\u5b57\u54e1\u5de5\u5728\u65b0\u589e\u81f3\u5c11\u4e00\u500b\u76ee\u9304\u524d\u7121\u6cd5\u50b3\u9001\u6a94\u6848\u3002",
              )}
            </p>
          )}
        </section>

        {!isPending && (
          <div className="ve-form-actions">
            <button
              className="ve-btn ve-btn--primary"
              onClick={handleSubmit}
              disabled={submitting || avatarPreparing}
              data-testid="ve-submit-btn"
            >
              {avatarPreparing ? c.avatarPreparing : submitting ? "..." : isRejected ? c.reapply : registered ? c.update : c.register}
            </button>
          </div>
        )}

        <section className="ve-form-section ve-form-section--approval">
          {/* Approval Capability Section (Requirements 3.1, 3.4, 3.5, 3.6, 3.7) */}
          <VEApprovalCapabilitySection lang={lang} footerSlot={approvalWorkflowDesign} />
        </section>
      </div>
    </div>
  );
}
