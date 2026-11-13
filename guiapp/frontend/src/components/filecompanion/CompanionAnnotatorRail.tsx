// The PDF reader's left rail, generalized: a bookmarks section on top and a
// notes section below, foldable down to a thin strip. Document and slide
// annotators feed it rows; it owns no store of its own.

import { useId, useState, type ReactNode } from "react";
import { companionSay, getCompanionUiLang } from "../../i18n/mnCompanion";

export type AnnotatorRailItem = {
    id: string;
    /** The chip label — bookmark name or note name. */
    label: string;
    /** Optional leading number ("p3", line, slide). Hidden when empty. */
    page?: string;
    /** A small leading swatch/icon node. */
    swatch?: ReactNode;
    /** Row highlight, e.g. the bookmark on the current page. */
    here?: boolean;
    onJump: () => void;
};

function FoldGlyph({ collapsed }: { collapsed: boolean }) {
    return (
        <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            {collapsed ? <path d="m4 2 4 4-4 4" /> : <path d="m8 2-4 4 4 4" />}
        </svg>
    );
}

export function CompanionAnnotatorRail({
    lang,
    bookmarks,
    notes,
    testId,
}: {
    lang?: string;
    bookmarks: AnnotatorRailItem[];
    notes: AnnotatorRailItem[];
    testId?: string;
}) {
    const say = (zh: string) => companionSay(lang || getCompanionUiLang(), zh);
    const [collapsed, setCollapsed] = useState(false);
    const columnId = useId();
    const count = bookmarks.length + notes.length;
    const foldLabel = collapsed ? say("展开书签和批注") : say("折叠书签和批注");
    return (
        <div
            className="companion-pdf-bookmark-column companion-annotator-rail"
            data-testid={testId ?? "annotator-rail"}
            data-collapsed={collapsed ? "true" : "false"}
            style={{ flexBasis: collapsed ? 24 : 156, width: collapsed ? 24 : 156 }}
            onClick={(event) => {
                if (!collapsed || event.target !== event.currentTarget) return;
                setCollapsed(false);
            }}
        >
            <button
                type="button"
                className="companion-pdf-bookmark-fold"
                data-testid="annotator-rail-fold"
                aria-expanded={!collapsed}
                aria-controls={columnId}
                aria-label={foldLabel}
                title={foldLabel}
                onPointerDown={(event) => event.stopPropagation()}
                onClick={(event) => {
                    event.stopPropagation();
                    setCollapsed((current) => !current);
                }}
            >
                <FoldGlyph collapsed={collapsed} />
            </button>
            <div className="companion-pdf-bookmark-body" id={columnId} hidden={collapsed}>
                {bookmarks.length > 0 ? (
                    <section className={notes.length > 0
                        ? "companion-pdf-rail-section companion-pdf-rail-section--bookmarks companion-pdf-rail-section--capped"
                        : "companion-pdf-rail-section companion-pdf-rail-section--bookmarks"}>
                        <p className="companion-pdf-rail-title">
                            {say("书签")}
                            <span className="companion-pdf-rail-count">{bookmarks.length}</span>
                        </p>
                        <div className="companion-pdf-bookmarks" role="list" aria-label={say("书签")}>
                            {bookmarks.map((item) => (
                                <button
                                    key={item.id}
                                    type="button"
                                    role="listitem"
                                    className={item.here ? "companion-pdf-bookmark companion-pdf-bookmark--here" : "companion-pdf-bookmark"}
                                    data-testid={`annotator-rail-bookmark-${item.id}`}
                                    title={item.label}
                                    onClick={(event) => {
                                        event.preventDefault();
                                        item.onJump();
                                    }}
                                >
                                    {item.page ? <span className="companion-pdf-bookmark-page">{item.page}</span> : null}
                                    {item.swatch ? <span className="companion-pdf-bookmark-swatch">{item.swatch}</span> : null}
                                    <span className="companion-pdf-bookmark-name">{item.label}</span>
                                </button>
                            ))}
                        </div>
                    </section>
                ) : null}
                {notes.length > 0 ? (
                    <section className={bookmarks.length > 0
                        ? "companion-pdf-rail-section companion-pdf-rail-section--annotations companion-pdf-rail-section--divided"
                        : "companion-pdf-rail-section companion-pdf-rail-section--annotations"}>
                        <p className="companion-pdf-rail-title">
                            {say("批注")}
                            <span className="companion-pdf-rail-count">{notes.length}</span>
                        </p>
                        <div className="companion-pdf-annotations" role="list" aria-label={say("批注")}>
                            {notes.map((item) => (
                                <button
                                    key={item.id}
                                    type="button"
                                    role="listitem"
                                    className={item.here ? "companion-pdf-note-item companion-pdf-note-item--here" : "companion-pdf-note-item"}
                                    data-testid={`annotator-rail-note-${item.id}`}
                                    title={item.label}
                                    onClick={(event) => {
                                        event.preventDefault();
                                        item.onJump();
                                    }}
                                >
                                    {item.page ? <span className="companion-pdf-bookmark-page">{item.page}</span> : null}
                                    <span className="companion-pdf-note-swatch" aria-hidden="true">
                                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                                            <path d="M4.5 4.5h15v11h-8.5L7 19.5v-4H4.5z" />
                                            <path d="M7.5 8.5h9" />
                                            <path d="M7.5 11.5h6" />
                                        </svg>
                                    </span>
                                    <span className="companion-pdf-note-name">{item.label}</span>
                                </button>
                            ))}
                        </div>
                    </section>
                ) : null}
                {count === 0 ? <p className="companion-pdf-bookmark-cap">{say("还没有书签或批注")}</p> : null}
            </div>
        </div>
    );
}
