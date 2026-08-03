// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { corelib, main } from '../../../../wailsjs/go/models';
import {
  CodingKnowledgeCapacity,
  CodingKnowledgeConfirm,
  CodingKnowledgeDelete,
  CodingKnowledgeEvict,
  CodingKnowledgeExportToFile,
  CodingKnowledgeGet,
  CodingKnowledgeGraduateToSteering,
  CodingKnowledgeImportFromFile,
  CodingKnowledgeList,
  CodingKnowledgeResetFile,
  CodingKnowledgeSearch,
  CodingKnowledgeStats,
  CodingKnowledgeUpdate,
  PatchConfigFields,
  SelectCodingKnowledgeExportPath,
  SelectCodingKnowledgeImportFile,
} from "../../../../wailsjs/go/main/App";
import { ProgrammingToolsSettingsPanel } from "../ProgrammingToolsSettingsPanel";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  LoadConfig: vi.fn(async () => new corelib.AppConfig({} as any)),
  PatchConfigFields: vi.fn(async (patch: Record<string, unknown>) => new corelib.AppConfig({
    coding_knowledge_max_total: 1000,
    coding_knowledge_max_per_project: 200,
    ...patch,
  } as any)),
  SetDefaultLaunchMode: vi.fn(async () => undefined),
  CodingKnowledgeStats: vi.fn(async () => ({
    total_count: 2,
    verified_count: 1,
    active_count: 1,
    candidate_count: 0,
  })),
  CodingKnowledgeCapacity: vi.fn(async () => ({
    total_count: 2,
    max_total: 1000,
    max_per_project: 200,
    over_total: 0,
    would_evict: 0,
    within_limit: true,
    projects_over: [],
  })),
  CodingKnowledgeList: vi.fn(async () => [
    {
      id: "exp-1",
      title: "Prefer context.WithTimeout for DB calls",
      category: "pattern",
      scope: "language",
      language: "go",
      status: "active",
      recall_count: 3,
    },
    {
      id: "exp-2",
      title: "Avoid panic in library code",
      category: "pitfall",
      scope: "universal",
      status: "candidate",
      recall_count: 0,
    },
    {
      id: "exp-3",
      title: "Verified retry pattern",
      category: "pattern",
      scope: "universal",
      status: "verified",
      recall_count: 8,
    },
  ]),
  CodingKnowledgeSearch: vi.fn(async () => [
    {
      id: "exp-1",
      title: "Prefer context.WithTimeout for DB calls",
      category: "pattern",
      scope: "language",
      language: "go",
      status: "active",
      recall_count: 3,
    },
  ]),
  CodingKnowledgeGet: vi.fn(async (id: string) => ({
    id,
    title: "Prefer context.WithTimeout for DB calls",
    category: "pattern",
    scope: "language",
    language: "go",
    status: "active",
    trigger_condition: "db timeout",
    content: "Always wrap DB calls with context.WithTimeout.",
    code_snippet: "ctx, cancel := context.WithTimeout(...)",
    confidence: 1.2,
    recall_count: 3,
    success_count: 2,
    failure_count: 1,
  })),
  CodingKnowledgeUpdate: vi.fn(async () => undefined),
  CodingKnowledgeDelete: vi.fn(async () => undefined),
  CodingKnowledgeConfirm: vi.fn(async () => undefined),
  CodingKnowledgeResetFile: vi.fn(async () => undefined),
  CodingKnowledgeExportToFile: vi.fn(async () => undefined),
  CodingKnowledgeImportFromFile: vi.fn(async () => 2),
  CodingKnowledgeGraduateToSteering: vi.fn(async () => "C:/Users/test/.maclaw/steering/exp.md"),
  CodingKnowledgeEvict: vi.fn(async () => 3),
  SelectCodingKnowledgeExportPath: vi.fn(async () => "D:/tmp/coding-pack.json"),
  SelectCodingKnowledgeImportFile: vi.fn(async () => "D:/tmp/import-pack.json"),
}));

vi.mock("../../CustomDialog", () => ({
  useDialog: () => ({
    showAlert: vi.fn(),
    showConfirm: vi.fn(async () => true),
    showPrompt: vi.fn(async () => null),
  }),
}));

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  cleanup();
});

describe("ProgrammingToolsSettingsPanel coding knowledge", () => {
  it("loads stats and experiences from coding knowledge bindings", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({ coding_knowledge_auto_save_mode: "observe" } as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    await waitFor(() => expect(CodingKnowledgeStats).toHaveBeenCalled());
    await waitFor(() => expect(CodingKnowledgeCapacity).toHaveBeenCalled());
    await waitFor(() => expect(CodingKnowledgeList).toHaveBeenCalled());
    expect(await screen.findByText("Prefer context.WithTimeout for DB calls")).toBeTruthy();
    expect(screen.getByText("Avoid panic in library code")).toBeTruthy();
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByText("/1000")).toBeTruthy();
  });

  it("searches coding knowledge with the dedicated API", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    await waitFor(() => expect(CodingKnowledgeList).toHaveBeenCalled());

    const search = screen.getByLabelText("Search knowledge base");
    fireEvent.change(search, { target: { value: "timeout" } });

    await waitFor(() => {
      expect(CodingKnowledgeSearch).toHaveBeenCalledWith("timeout", 50);
    });
  });

  it("opens the editor and saves experience updates", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    const editButtons = await screen.findAllByText("Edit");
    fireEvent.click(editButtons[0]);

    await waitFor(() => expect(CodingKnowledgeGet).toHaveBeenCalledWith("exp-1"));
    expect(await screen.findByText("Edit Experience")).toBeTruthy();
    expect(screen.getByDisplayValue("Always wrap DB calls with context.WithTimeout.")).toBeTruthy();

    fireEvent.change(screen.getByDisplayValue("Prefer context.WithTimeout for DB calls"), {
      target: { value: "Prefer timeouts for every DB call" },
    });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(CodingKnowledgeUpdate).toHaveBeenCalledWith(
        expect.objectContaining({
          id: "exp-1",
          title: "Prefer timeouts for every DB call",
          content: "Always wrap DB calls with context.WithTimeout.",
        }),
      );
    });
    await waitFor(() => expect(screen.getByText("Experience saved.")).toBeTruthy());
  });

  it("exports and imports experience packs", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    await waitFor(() => expect(CodingKnowledgeList).toHaveBeenCalled());
    fireEvent.click(screen.getByText("Export"));
    await waitFor(() => {
      expect(SelectCodingKnowledgeExportPath).toHaveBeenCalled();
      expect(CodingKnowledgeExportToFile).toHaveBeenCalledWith("D:/tmp/coding-pack.json");
    });

    fireEvent.click(screen.getByText("Import"));
    await waitFor(() => {
      expect(SelectCodingKnowledgeImportFile).toHaveBeenCalled();
      expect(CodingKnowledgeImportFromFile).toHaveBeenCalledWith("D:/tmp/import-pack.json");
    });
    expect(await screen.findByText("Imported 2 experiences.")).toBeTruthy();
  });

  it("graduates a verified experience", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    const graduateButtons = await screen.findAllByText("Graduate");
    fireEvent.click(graduateButtons[0]);

    await waitFor(() => {
      expect(CodingKnowledgeGraduateToSteering).toHaveBeenCalledWith("exp-3");
    });
  });

  it("confirms a candidate experience", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    const confirmBtn = await screen.findByText("Confirm");
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(CodingKnowledgeConfirm).toHaveBeenCalledWith("exp-2");
    });
  });

  it("deletes an experience after confirm", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    const deleteButtons = await screen.findAllByText("Delete");
    fireEvent.click(deleteButtons[0]);

    await waitFor(() => {
      expect(CodingKnowledgeDelete).toHaveBeenCalledWith("exp-1");
    });
  });

  it("resets the coding knowledge file", async () => {
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({} as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    await waitFor(() => expect(CodingKnowledgeList).toHaveBeenCalled());
    const reset = screen.getByLabelText("Clear all knowledge");
    fireEvent.click(reset);

    await waitFor(() => {
      expect(CodingKnowledgeResetFile).toHaveBeenCalled();
    });
  });

  it("patches capacity limits and runs eviction", async () => {
    const setConfig = vi.fn();
    render(
      <ProgrammingToolsSettingsPanel
        config={new corelib.AppConfig({
          coding_knowledge_max_total: 1000,
          coding_knowledge_max_per_project: 200,
        } as any)}
        setConfig={setConfig}
        lang="en"
      />
    );

    await waitFor(() => expect(CodingKnowledgeCapacity).toHaveBeenCalled());

    const maxTotal = screen.getByLabelText("Max total") as HTMLInputElement;
    fireEvent.change(maxTotal, { target: { value: "50" } });
    await waitFor(() => {
      expect(PatchConfigFields).toHaveBeenCalledWith(
        expect.objectContaining({ coding_knowledge_max_total: 50 }),
      );
    });

    fireEvent.click(screen.getByText("Run eviction"));
    await waitFor(() => {
      expect(CodingKnowledgeEvict).toHaveBeenCalled();
    });
    expect(await screen.findByText("Evicted 3 experiences.")).toBeTruthy();
  });
});
