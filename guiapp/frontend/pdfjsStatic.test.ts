import {existsSync, mkdtempSync, rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {afterEach, describe, expect, it} from "vitest";
import type {IncomingMessage, ServerResponse} from "node:http";
import {installPdfjsStatic, pdfjsPackageDir, pdfjsStaticContentType, pdfjsStaticMiddleware, pdfjsStaticRequestPath, resolvePdfjsStatic} from "./pdfjsStatic";

const pkg = pdfjsPackageDir();
const temps: string[] = [];

afterEach(() => {
    while (temps.length) {
        const dir = temps.pop();
        if (dir) rmSync(dir, {recursive: true, force: true});
    }
});

describe("resolvePdfjsStatic", () => {
    it("points the worker and a packed cmap at the package files", () => {
        const worker = resolvePdfjsStatic(pkg, "/pdfjs/pdf.worker.min.mjs");
        const cmap = resolvePdfjsStatic(pkg, "/pdfjs/cmaps/UniGB-UCS2-H.bcmap?cache=1");
        const font = resolvePdfjsStatic(pkg, "/pdfjs/standard_fonts/LiberationSans-Regular.ttf");
        expect(worker && existsSync(worker)).toBe(true);
        expect(cmap && existsSync(cmap)).toBe(true);
        expect(font && existsSync(font)).toBe(true);
        expect(pdfjsStaticContentType(worker || "")).toBe("text/javascript; charset=utf-8");
        expect(pdfjsStaticContentType(cmap || "")).toBe("application/octet-stream");
    });

    it("rejects paths that leave the package directories", () => {
        expect(resolvePdfjsStatic(pkg, "/pdfjs/cmaps/../package.json")).toBeNull();
        expect(resolvePdfjsStatic(pkg, "/pdfjs/cmaps/%2e%2e%2fpackage.json")).toBeNull();
        expect(resolvePdfjsStatic(pkg, "/pdfjs/cmaps/..")).toBeNull();
        expect(resolvePdfjsStatic(pkg, "/pdfjs/pdf.worker.min.js")).toBeNull();
        expect(resolvePdfjsStatic(pkg, "/pdfjs/pdf.worker.min.mjs.map")).toBeNull();
        expect(resolvePdfjsStatic(pkg, "/other/pdf.worker.min.mjs")).toBeNull();
        expect(pdfjsStaticRequestPath("/pdfjs/pdf.worker.min.mjs.map")).toBe("/pdfjs/pdf.worker.min.mjs.map");
        expect(pdfjsStaticRequestPath("/other/pdf.worker.min.mjs")).toBeNull();
    });
});

describe("pdfjsStaticMiddleware", () => {
    function dispatch(url: string) {
        let ended = false;
        let forwarded = false;
        const res = {
            statusCode: 0,
            setHeader() {},
            end() {
                ended = true;
            },
        };
        pdfjsStaticMiddleware(pkg)({url} as IncomingMessage, res as unknown as ServerResponse, () => {
            forwarded = true;
        });
        return {statusCode: res.statusCode, ended, forwarded};
    }

    it("answers a /pdfjs/ path that is not a package file with 404", () => {
        const missed = dispatch("/pdfjs/pdf.worker.min.mjs.map");
        expect(missed.forwarded).toBe(false);
        expect(missed.statusCode).toBe(404);
        expect(missed.ended).toBe(true);

        const escaped = dispatch("/pdfjs/cmaps/%2e%2e%2fpackage.json");
        expect(escaped.forwarded).toBe(false);
        expect(escaped.statusCode).toBe(404);

        const broken = dispatch("/pdfjs/%E0%A4%A");
        expect(broken.forwarded).toBe(false);
        expect(broken.statusCode).toBe(404);
    });

    it("leaves paths outside /pdfjs/ to the app", () => {
        const other = dispatch("/other/pdf.worker.min.mjs");
        expect(other.forwarded).toBe(true);
        expect(other.ended).toBe(false);
    });
});

describe("installPdfjsStatic", () => {
    it("copies the worker, a chinese cmap, and a standard font into the build output", () => {
        const out = mkdtempSync(join(tmpdir(), "pdfjs-static-"));
        temps.push(out);
        installPdfjsStatic(out, pkg);
        expect(existsSync(join(out, "pdfjs", "pdf.worker.min.mjs"))).toBe(true);
        expect(existsSync(join(out, "pdfjs", "cmaps", "UniGB-UCS2-H.bcmap"))).toBe(true);
        expect(existsSync(join(out, "pdfjs", "standard_fonts", "LiberationSans-Regular.ttf"))).toBe(true);
    });
});
