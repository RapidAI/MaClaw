// Preview-only shim: hand AssistantMermaidDiagram the UMD global that
// mermaid.min.js already put on window, instead of bundling the 3MB ESM build.
export default (globalThis as any).mermaid;
