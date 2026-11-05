import { describe, expect, it } from "vitest";
import { webPreviewTargetFromFile } from "../preview/webPreviewAddress";
import {
    chatImagePreviewIdentity,
    documentMarkdownImageSrc,
    parseMarkdownImageAt,
    parseMarkdownLinkAt,
    safeMarkdownImageSrc,
    safeRemoteImageURL,
    trimAutolink,
    visibleMarkdownLinkLabel,
} from "./markdownMedia";

describe("chat image and link targets", () => {
    it("keeps a cited image off the web-preview and disk-file paths", () => {
        const identity = chatImagePreviewIdentity("https://upload.wikimedia.org/wikipedia/commons/a/ab/Foo_(bar).png", "roofline 图");
        expect(identity.filePath.startsWith("chat-image-")).toBe(true);
        expect(identity.filePath.endsWith(".png")).toBe(true);
        expect(identity.filePath.includes("://")).toBe(false);
        expect(identity.remoteImageURL).toContain("Foo_(bar).png");
        expect(webPreviewTargetFromFile({
            filePath: identity.filePath,
            fileName: identity.fileName,
            content: "",
            language: "image",
        })).toBeNull();
    });

    it("rejects script and file image sources", () => {
        expect(safeMarkdownImageSrc("javascript:alert(1)")).toBe("");
        expect(safeMarkdownImageSrc("file:///tmp/secret.png")).toBe("");
        expect(safeRemoteImageURL("https://user:pass@example.com/a.png")).toBe("");
    });

    it("reads an image URL that contains parentheses", () => {
        const src = "https://upload.wikimedia.org/wikipedia/commons/a/ab/Foo_(bar).png";
        const parsed = parseMarkdownImageAt(`![roofline](${src})`, 0);
        expect(parsed?.src).toContain("Foo_(bar).png");
        expect(parsed?.alt).toBe("roofline");
    });

    it("rejects a protocol-relative document image", () => {
        expect(documentMarkdownImageSrc("//evil.example/pixel.png")).toBe("");
        expect(documentMarkdownImageSrc("./figures/roofline.png")).toBe("./figures/roofline.png");
    });

    it("keeps a closing parenthesis that belongs to the link URL", () => {
        const href = "https://upload.wikimedia.org/wikipedia/commons/a/ab/Foo_(bar).png";
        const parsed = parseMarkdownLinkAt(`[figure](${href})`, 0);
        expect(parsed?.href).toContain("Foo_(bar).png");
        expect(parsed?.label).toBe("figure");
    });

    it("drops extra closing parentheses around an autolink without eating a balanced one", () => {
        expect(trimAutolink("https://example.com/a)).")).toBe("https://example.com/a");
        expect(trimAutolink("https://example.com/Foo_(bar).png")).toBe("https://example.com/Foo_(bar).png");
        expect(trimAutolink("https://example.com/foo）")).toBe("https://example.com/foo");
    });

    it("shows the URL when a link label is only emphasis marks", () => {
        expect(visibleMarkdownLinkLabel("**", "https://en.wikipedia.org/wiki/Roofline_model")).toBe("https://en.wikipedia.org/wiki/Roofline_model");
        expect(visibleMarkdownLinkLabel("**Roofline model**", "https://example.com")).toBe("Roofline model");
    });
});
