import { windowDragHandleProps } from "../../utils/windowDrag";

export function AssistantDragHandle() {
    return (
        <div {...windowDragHandleProps(true, {
            height: "30px",
            width: "100%",
            position: "absolute",
            top: 0,
            left: 0,
            zIndex: 999,
            userSelect: "none",
        })} />
    );
}
