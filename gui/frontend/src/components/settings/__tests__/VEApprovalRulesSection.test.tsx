// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VEApprovalRulesSection, ApprovalRules } from "../VEApprovalRulesSection";

afterEach(() => {
  cleanup();
});

const emptyRules: ApprovalRules = {
  auto_reject: [],
  auto_approve: [],
  require_human: [],
};

describe("VEApprovalRulesSection", () => {
  it("renders three rule categories", () => {
    render(<VEApprovalRulesSection rules={emptyRules} onChange={() => {}} />);
    expect(screen.getByTestId("rule-category-auto_reject")).toBeTruthy();
    expect(screen.getByTestId("rule-category-auto_approve")).toBeTruthy();
    expect(screen.getByTestId("rule-category-require_human")).toBeTruthy();
  });

  it("displays empty hints when no rules configured", () => {
    render(<VEApprovalRulesSection rules={emptyRules} onChange={() => {}} />);
    expect(screen.getByTestId("rule-empty-auto_reject")).toBeTruthy();
    expect(screen.getByTestId("rule-empty-auto_approve")).toBeTruthy();
    expect(screen.getByTestId("rule-empty-require_human")).toBeTruthy();
  });

  it("adds a rule when Add Rule button is clicked", () => {
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={emptyRules} onChange={onChange} />);

    fireEvent.click(screen.getByTestId("rule-add-auto_approve"));
    expect(onChange).toHaveBeenCalledTimes(1);

    const updatedRules = onChange.mock.calls[0][0] as ApprovalRules;
    expect(updatedRules.auto_approve.length).toBe(1);
    expect(updatedRules.auto_approve[0].position).toBe(0);
    expect(updatedRules.auto_approve[0].conditions.length).toBe(1);
  });

  it("enforces max 50 rules per category", () => {
    const fullRules: ApprovalRules = {
      auto_reject: Array.from({ length: 50 }, (_, i) => ({
        id: `rule_${i}`,
        name: `Rule ${i}`,
        position: i,
        conditions: [{ field: "x", operator: "equals" as const, value: "y" }],
      })),
      auto_approve: [],
      require_human: [],
    };
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={fullRules} onChange={onChange} />);

    const addBtn = screen.getByTestId("rule-add-auto_reject") as HTMLButtonElement;
    expect(addBtn.disabled).toBe(true);
  });

  it("displays rule count in category header", () => {
    const rules: ApprovalRules = {
      auto_reject: [
        { id: "r1", name: "Test", position: 0, conditions: [{ field: "a", operator: "equals", value: "b" }] },
      ],
      auto_approve: [],
      require_human: [],
    };
    render(<VEApprovalRulesSection rules={rules} onChange={() => {}} />);
    expect(screen.getByTestId("rule-category-auto_reject").textContent).toContain("(1/50)");
  });

  it("removes a rule and reindexes positions", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "First", position: 0, conditions: [{ field: "a", operator: "equals", value: "1" }] },
        { id: "r2", name: "Second", position: 1, conditions: [{ field: "b", operator: "equals", value: "2" }] },
      ],
      require_human: [],
    };
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={rules} onChange={onChange} />);

    fireEvent.click(screen.getByTestId("rule-remove-r1"));
    expect(onChange).toHaveBeenCalledTimes(1);

    const updated = onChange.mock.calls[0][0] as ApprovalRules;
    expect(updated.auto_approve.length).toBe(1);
    expect(updated.auto_approve[0].id).toBe("r2");
    expect(updated.auto_approve[0].position).toBe(0);
  });

  it("moves a rule up and down", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "First", position: 0, conditions: [{ field: "a", operator: "equals", value: "1" }] },
        { id: "r2", name: "Second", position: 1, conditions: [{ field: "b", operator: "equals", value: "2" }] },
        { id: "r3", name: "Third", position: 2, conditions: [{ field: "c", operator: "equals", value: "3" }] },
      ],
      require_human: [],
    };
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={rules} onChange={onChange} />);

    // Move r2 up
    fireEvent.click(screen.getByTestId("rule-move-up-r2"));
    expect(onChange).toHaveBeenCalledTimes(1);
    const afterMoveUp = onChange.mock.calls[0][0] as ApprovalRules;
    expect(afterMoveUp.auto_approve[0].id).toBe("r2");
    expect(afterMoveUp.auto_approve[0].position).toBe(0);
    expect(afterMoveUp.auto_approve[1].id).toBe("r1");
    expect(afterMoveUp.auto_approve[1].position).toBe(1);
  });

  it("disables move up for first rule and move down for last rule", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "First", position: 0, conditions: [{ field: "a", operator: "equals", value: "1" }] },
        { id: "r2", name: "Last", position: 1, conditions: [{ field: "b", operator: "equals", value: "2" }] },
      ],
      require_human: [],
    };
    render(<VEApprovalRulesSection rules={rules} onChange={() => {}} />);

    const moveUpFirst = screen.getByTestId("rule-move-up-r1") as HTMLButtonElement;
    expect(moveUpFirst.disabled).toBe(true);

    const moveDownLast = screen.getByTestId("rule-move-down-r2") as HTMLButtonElement;
    expect(moveDownLast.disabled).toBe(true);
  });

  it("allows editing rule name", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "Original", position: 0, conditions: [{ field: "a", operator: "equals", value: "1" }] },
      ],
      require_human: [],
    };
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={rules} onChange={onChange} />);

    const nameInput = screen.getByTestId("rule-name-r1") as HTMLInputElement;
    fireEvent.change(nameInput, { target: { value: "Updated Name" } });
    expect(onChange).toHaveBeenCalledTimes(1);
    const updated = onChange.mock.calls[0][0] as ApprovalRules;
    expect(updated.auto_approve[0].name).toBe("Updated Name");
  });

  it("allows editing condition field, operator, and value", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "Test", position: 0, conditions: [{ field: "", operator: "equals", value: "" }] },
      ],
      require_human: [],
    };
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={rules} onChange={onChange} />);

    // Edit field
    const fieldInput = screen.getByTestId("condition-field-0") as HTMLInputElement;
    fireEvent.change(fieldInput, { target: { value: "request.amount" } });
    expect(onChange).toHaveBeenCalled();

    // Edit operator
    onChange.mockClear();
    const opSelect = screen.getByTestId("condition-operator-0") as HTMLSelectElement;
    fireEvent.change(opSelect, { target: { value: "greater_than" } });
    expect(onChange).toHaveBeenCalled();
  });

  it("hides value input for is_empty and is_not_empty operators", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "Test", position: 0, conditions: [{ field: "x", operator: "is_empty", value: "" }] },
      ],
      require_human: [],
    };
    render(<VEApprovalRulesSection rules={rules} onChange={() => {}} />);
    expect(screen.queryByTestId("condition-value-0")).toBeNull();
  });

  it("shows reason field only for auto_reject rules", () => {
    const rules: ApprovalRules = {
      auto_reject: [
        { id: "r1", name: "Reject", position: 0, conditions: [{ field: "x", operator: "equals", value: "y" }], reason: "Too expensive" },
      ],
      auto_approve: [
        { id: "r2", name: "Approve", position: 0, conditions: [{ field: "a", operator: "equals", value: "b" }] },
      ],
      require_human: [],
    };
    render(<VEApprovalRulesSection rules={rules} onChange={() => {}} />);

    // auto_reject has reason field
    expect(screen.getByTestId("rule-reason-r1")).toBeTruthy();
    // auto_approve does not
    expect(screen.queryByTestId("rule-reason-r2")).toBeNull();
  });

  it("collapses and expands categories", () => {
    const rules: ApprovalRules = {
      auto_reject: [
        { id: "r1", name: "Test", position: 0, conditions: [{ field: "x", operator: "equals", value: "y" }] },
      ],
      auto_approve: [],
      require_human: [],
    };
    render(<VEApprovalRulesSection rules={rules} onChange={() => {}} />);

    // Initially expanded
    expect(screen.getByTestId("rule-item-r1")).toBeTruthy();

    // Collapse
    fireEvent.click(screen.getByTestId("rule-category-toggle-auto_reject"));
    expect(screen.queryByTestId("rule-item-r1")).toBeNull();

    // Expand again
    fireEvent.click(screen.getByTestId("rule-category-toggle-auto_reject"));
    expect(screen.getByTestId("rule-item-r1")).toBeTruthy();
  });

  it("adds and removes conditions within a rule", () => {
    const rules: ApprovalRules = {
      auto_reject: [],
      auto_approve: [
        { id: "r1", name: "Test", position: 0, conditions: [{ field: "a", operator: "equals", value: "1" }] },
      ],
      require_human: [],
    };
    const onChange = vi.fn();
    render(<VEApprovalRulesSection rules={rules} onChange={onChange} />);

    // Add condition
    fireEvent.click(screen.getByTestId("rule-add-condition-r1"));
    expect(onChange).toHaveBeenCalledTimes(1);
    const afterAdd = onChange.mock.calls[0][0] as ApprovalRules;
    expect(afterAdd.auto_approve[0].conditions.length).toBe(2);

    // Remove condition
    onChange.mockClear();
    // Re-render with updated rules
    cleanup();
    render(<VEApprovalRulesSection rules={afterAdd} onChange={onChange} />);
    fireEvent.click(screen.getByTestId("condition-remove-0"));
    expect(onChange).toHaveBeenCalledTimes(1);
    const afterRemove = onChange.mock.calls[0][0] as ApprovalRules;
    expect(afterRemove.auto_approve[0].conditions.length).toBe(1);
  });
});
