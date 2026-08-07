export function AssistantDragHandle() {
    return (
        <div data-window-drag style={{
            height: "30px", width: "100%",
            position: "absolute", top: 0, left: 0, zIndex: 999,
            userSelect: "none",
            '--wails-draggable': 'drag',
        } as any} />
    );
}
