import { describe, expect, it } from "vitest";
import {
    applyClosePanel,
    applyFocusPreviewFile,
    applyFileUpdate,
    codeFileLineDeltaHasChange,
    computeCodeFileLineDelta,
    formatCodeFileLineDelta,
    initialState,
    resolveCodePreviewFilePath,
    splitCodeFileLines,
    type CodeFile,
} from "../useCodePreviewState";

function previewFile(partial: Partial<CodeFile> & Pick<CodeFile, "filePath" | "content" | "opType">): CodeFile {
    return {
        fileName: partial.fileName || partial.filePath.split(/[/\\]/).pop() || partial.filePath,
        language: "cpp",
        updatedAt: 1,
        ...partial,
    };
}

describe("computeCodeFileLineDelta", () => {
    it("treats reads as unchanged", () => {
        expect(computeCodeFileLineDelta({
            opType: "read",
            content: "a\nb\n",
            original: "x",
        })).toEqual({ added: 0, removed: 0 });
    });

    it("counts every create line as added", () => {
        expect(computeCodeFileLineDelta({
            opType: "create",
            content: "int main() {\n    return 0;\n}\n",
        })).toEqual({ added: 3, removed: 0 });
        expect(formatCodeFileLineDelta({ added: 3, removed: 0 })).toBe("+3 -0");
    });

    it("uses bag-of-lines for modifications", () => {
        expect(computeCodeFileLineDelta({
            opType: "modify",
            original: "keep\nold\nkeep\n",
            content: "keep\nnew\nkeep\nextra\n",
        })).toEqual({ added: 2, removed: 1 });
        expect(formatCodeFileLineDelta({ added: 2, removed: 1 })).toBe("+2 -1");
    });

    it("returns empty label when both sides are zero", () => {
        expect(codeFileLineDeltaHasChange({ added: 0, removed: 0 })).toBe(false);
        expect(formatCodeFileLineDelta({ added: 0, removed: 0 })).toBe("");
        expect(splitCodeFileLines("hello\n")).toEqual(["hello"]);
    });
});

describe("resolveCodePreviewFilePath / applyFocusPreviewFile", () => {
    it("matches basename, suffix, and absPath uniquely", () => {
        const files = new Map<string, CodeFile>([
            ["/src/hello_world.cpp", previewFile({
                filePath: "/src/hello_world.cpp",
                absPath: "D:\\proj\\src\\hello_world.cpp",
                content: "int main() {}",
                opType: "create",
            })],
            ["/src/util.go", previewFile({
                filePath: "/src/util.go",
                content: "package src",
                opType: "modify",
                original: "package old",
            })],
        ]);
        expect(resolveCodePreviewFilePath("hello_world.cpp", files)).toBe("/src/hello_world.cpp");
        expect(resolveCodePreviewFilePath("src/hello_world.cpp", files)).toBe("/src/hello_world.cpp");
        expect(resolveCodePreviewFilePath("D:\\proj\\src\\hello_world.cpp", files)).toBe("/src/hello_world.cpp");
        expect(resolveCodePreviewFilePath("missing.go", files)).toBeUndefined();
    });

    it("does not guess when two files share a basename", () => {
        const files = new Map<string, CodeFile>([
            ["/a/main.go", previewFile({ filePath: "/a/main.go", content: "a", opType: "read" })],
            ["/b/main.go", previewFile({ filePath: "/b/main.go", content: "b", opType: "read" })],
        ]);
        expect(resolveCodePreviewFilePath("main.go", files)).toBeUndefined();
        expect(resolveCodePreviewFilePath("a/main.go", files)).toBe("/a/main.go");
    });

    it("reopens a closed preview on an explicit trail click", () => {
        let state = initialState();
        state = applyFileUpdate(state, previewFile({
            filePath: "/src/hello_world.cpp",
            content: "int main() {}",
            opType: "create",
            forceOpen: true,
            sessionID: "s1",
        }));
        state = applyClosePanel(state);
        expect(state.active).toBe(false);
        expect(state.userClosed).toBe(true);

        const focused = applyFocusPreviewFile(state, "hello_world.cpp");
        expect(focused.active).toBe(true);
        expect(focused.userClosed).toBe(false);
        expect(focused.activeFilePath).toBe("/src/hello_world.cpp");
        expect(applyFocusPreviewFile(state, "nope.cpp")).toBe(state);
    });
});
