import { describe, expect, it } from "vitest";
import { normalizeNotificationLocales, pickNotificationText } from "./notificationLocale";

const item = {
  title: "你好",
  content: "正文",
  button_text: "查看详情",
  source_lang: "zh-Hans",
  locales: {
    "zh-Hans": { title: "你好", content: "正文", button_text: "查看详情" },
    en: { title: "Hello", content: "Body", button_text: "View" },
    "zh-Hant": { title: "你好", content: "", button_text: "" },
  },
};

describe("pickNotificationText", () => {
  it("shows the current interface language", () => {
    expect(pickNotificationText("en-US", item)).toEqual({
      title: "Hello",
      content: "Body",
      button_text: "View",
    });
  });

  it("falls back to the original when that language is missing", () => {
    expect(pickNotificationText("mn", item)).toEqual({
      title: "你好",
      content: "正文",
      button_text: "查看详情",
    });
  });

  it("fills an empty field from the original", () => {
    expect(pickNotificationText("zh-Hant", item)).toEqual({
      title: "你好",
      content: "正文",
      button_text: "查看详情",
    });
  });

  it("keeps the original when the copy has no title and no body", () => {
    expect(pickNotificationText("en", {
      title: "原文",
      content: "内容",
      button_text: "打开",
      locales: { en: { title: "  ", content: "", button_text: "Open" } },
    })).toEqual({ title: "原文", content: "内容", button_text: "打开" });
  });
});

describe("normalizeNotificationLocales", () => {
  it("keeps title, content, and button", () => {
    expect(normalizeNotificationLocales({
      en: { title: "Hello", content: "Body", button_text: "View", extra: 1 },
    })).toEqual({
      en: { title: "Hello", content: "Body", button_text: "View" },
    });
  });

  it("drops a non-object", () => {
    expect(normalizeNotificationLocales("en")).toBeUndefined();
  });
});
