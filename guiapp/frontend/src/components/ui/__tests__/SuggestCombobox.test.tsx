// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { SuggestCombobox } from "../SuggestCombobox";
import { filterSuggestOptions, shouldIgnoreSuggestInput } from "../suggestOptions";

describe("filterSuggestOptions", () => {
    const options = [
        { value: "default-model" },
        { value: "gpt-5.4" },
        { value: "glm-5.3", description: "GLM" },
    ];

    it("returns every option until the user types", () => {
        expect(filterSuggestOptions(options, null).map((option) => option.value)).toEqual([
            "default-model",
            "gpt-5.4",
            "glm-5.3",
        ]);
        expect(filterSuggestOptions(options, "   ").map((option) => option.value)).toEqual([
            "default-model",
            "gpt-5.4",
            "glm-5.3",
        ]);
    });

    it("filters by the text the user typed", () => {
        expect(filterSuggestOptions(options, "GLM").map((option) => option.value)).toEqual(["glm-5.3"]);
        expect(filterSuggestOptions(options, "nope")).toEqual([]);
    });

    it("ignores the input event fired by opening the list", () => {
        expect(shouldIgnoreSuggestInput("default-model", "default-model")).toBe(true);
        expect(shouldIgnoreSuggestInput("default-model ", "default-model")).toBe(true);
        expect(shouldIgnoreSuggestInput("only-this", "default-model", "insertReplacementText")).toBe(true);
        expect(shouldIgnoreSuggestInput("gpt", "default-model", "insertText")).toBe(false);
        expect(shouldIgnoreSuggestInput("", "default-model", "deleteContentBackward")).toBe(false);
    });
});

function Harness() {
    const [value, setValue] = useState("default-model");
    return (
        <SuggestCombobox
            ariaLabel="Model"
            toggleLabel="Show model list"
            emptyText="No matching models"
            listboxId="model-options"
            openOnEnter
            value={value}
            options={["default-model", "gpt-5.4", "glm-5.3"]}
            onChange={setValue}
        />
    );
}

describe("SuggestCombobox", () => {
    it("shows every option from the dropdown button and filters only after typing", () => {
        render(<Harness />);
        const input = screen.getByLabelText("Model") as HTMLInputElement;
        expect(input.value).toBe("default-model");

        fireEvent.click(screen.getByRole("button", { name: "Show model list" }));
        fireEvent.change(input, { target: { value: "default-model" } });
        fireEvent.change(input, { target: { value: "default-model " } });
        expect(input.value).toBe("default-model");
        expect(screen.getAllByRole("option").map((option) => option.getAttribute("data-value"))).toEqual([
            "default-model",
            "gpt-5.4",
            "glm-5.3",
        ]);

        fireEvent.change(input, { target: { value: "gpt" } });
        expect(screen.getAllByRole("option").map((option) => option.getAttribute("data-value"))).toEqual(["gpt-5.4"]);

        fireEvent.change(input, { target: { value: "" } });
        expect(screen.getAllByRole("option").map((option) => option.getAttribute("data-value"))).toEqual([
            "default-model",
            "gpt-5.4",
            "glm-5.3",
        ]);
    });

    it("selects the current text from the dropdown button so the next key replaces it", () => {
        render(<Harness />);
        const input = screen.getByLabelText("Model") as HTMLInputElement;
        fireEvent.click(screen.getByRole("button", { name: "Show model list" }));
        expect(input.selectionStart).toBe(0);
        expect(input.selectionEnd).toBe("default-model".length);
        expect(document.getElementById("model-options")?.style.position).toBe("fixed");
    });

    it("keeps focus in the field while arrow keys move the highlight", () => {
        render(<Harness />);
        const input = screen.getByLabelText("Model") as HTMLInputElement;
        fireEvent.click(screen.getByRole("button", { name: "Show model list" }));
        expect(document.activeElement).toBe(input);
        fireEvent.keyDown(input, { key: "ArrowDown" });
        fireEvent.keyDown(input, { key: "Enter" });
        expect(input.value).toBe("gpt-5.4");
        expect(screen.queryByRole("listbox")).toBeNull();
    });

    it("shows every option again from the dropdown button while a typed filter is active", () => {
        render(<Harness />);
        const input = screen.getByLabelText("Model") as HTMLInputElement;
        fireEvent.click(screen.getByRole("button", { name: "Show model list" }));
        fireEvent.change(input, { target: { value: "gpt" } });
        expect(screen.getAllByRole("option").map((option) => option.getAttribute("data-value"))).toEqual(["gpt-5.4"]);

        fireEvent.click(screen.getByRole("button", { name: "Show model list" }));
        fireEvent.change(input, { target: { value: "gpt" } });
        expect(screen.getAllByRole("option").map((option) => option.getAttribute("data-value"))).toEqual([
            "default-model",
            "gpt-5.4",
            "glm-5.3",
        ]);

        fireEvent.click(screen.getByRole("button", { name: "Show model list" }));
        expect(screen.queryByRole("listbox")).toBeNull();
    });
});
