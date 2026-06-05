export function AssistantDragHandle() {
    return (
        <div style={{
            height: "30px", width: "100%",
            position: "absolute", top: 0, left: 0, zIndex: 999,
            '--wails-draggable': 'drag',
        } as any} />
    );
}
