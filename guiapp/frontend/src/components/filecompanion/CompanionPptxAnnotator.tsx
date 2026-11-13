// Annotations on a slide image. There is no selectable text in a .pptx page,
// so marks are dragged rectangles and notes anchor on the spot they were
// written — every geometry is a fraction of the picture, the same normalized
// space the PDF reader stores for its pages. The four stores are the PDF
// ones keyed by this file's path: a path is one kind, so slide highlights,
// drawings, notes, and bookmarks ride the share payload unchanged.

import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { companionSay, getCompanionUiLang } from "../../i18n/mnCompanion";
import { subscribeCompanionReaderState } from "./companionReaderState";
import {
    loadPdfMarks,
    newPdfMarkId,
    pdfMarkFill,
    pdfMarkPathKey,
    savePdfMarks,
    subscribePdfMarks,
    PDF_MARK_COLORS,
    type PdfMark,
    type PdfMarkColor,
    type PdfMarkRect,
} from "./pdfMarks";
import {
    loadPdfDrawings,
    savePdfDrawings,
    subscribePdfDrawings,
    type PdfDrawing,
    type PdfDrawingPoint,
} from "./pdfDrawings";
import {
    loadPdfBookmarks,
    newPdfBookmarkId,
    pdfBookmarkFill,
    pdfBookmarkInk,
    savePdfBookmarks,
    subscribePdfBookmarks,
    PDF_BOOKMARK_COLORS,
    PDF_BOOKMARK_TICK_PX,
    PDF_BOOKMARK_TICK_WIDTH_PX,
    type PdfBookmark,
    type PdfBookmarkColor,
} from "./pdfBookmarks";
import {
    clipPdfAnnotationBody,
    clipPdfAnnotationName,
    loadPdfAnnotations,
    newPdfTranslationId,
    notifyPdfAnnotationsChanged,
    savePdfTranslations,
    subscribePdfTranslations,
    loadPdfTranslations,
    type PdfTranslation,
} from "./pdfTranslations";
import { COMPANION_DRAWING_INK, CompanionDrawingLayer, type CompanionDrawTool } from "./CompanionDrawingLayer";
import { CompanionAnnotatorRail, type AnnotatorRailItem } from "./CompanionAnnotatorRail";

const MAX_MENU_BODY = 4000;

function say2(lang: string, zh: string): string {
    return companionSay(lang || getCompanionUiLang(), zh);
}

function pointOf(element: HTMLElement | null, clientX: number, clientY: number): PdfDrawingPoint | null {
    const box = element?.getBoundingClientRect();
    if (!box || box.width < 1 || box.height < 1) return null;
    return {
        x: Math.min(1, Math.max(0, (clientX - box.left) / box.width)),
        y: Math.min(1, Math.max(0, (clientY - box.top) / box.height)),
    };
}

function rectContains(rect: PdfMarkRect, point: PdfDrawingPoint): boolean {
    return point.x >= rect.x && point.x <= rect.x + rect.w && point.y >= rect.y && point.y <= rect.y + rect.h;
}

function markAt(marks: PdfMark[], page: number, point: PdfDrawingPoint): PdfMark | null {
    for (let index = marks.length - 1; index >= 0; index -= 1) {
        const mark = marks[index];
        if (mark.page !== page) continue;
        if (mark.rects.some((rect) => rectContains(rect, point))) return mark;
    }
    return null;
}

function clampAnchor(point: PdfDrawingPoint): PdfMarkRect {
    const w = 0.024;
    const h = 0.024;
    const x = Math.min(1 - w, Math.max(0, point.x - w / 2));
    const y = Math.min(1 - h, Math.max(0, point.y - h / 2));
    return { x, y, w, h };
}

function PptxMenuGlyph({ kind }: { kind: "note" | "pen" | "trash" | "bookmark" | "highlight" | "erase" }) {
    const common = {
        className: "companion-pdf-menu-glyph",
        viewBox: "0 0 24 24",
        fill: "none",
        stroke: "currentColor",
        strokeWidth: 1.7,
        strokeLinecap: "round" as const,
        strokeLinejoin: "round" as const,
        "aria-hidden": true,
    };
    if (kind === "trash") return <svg {...common}><path d="M5.5 6h13M9.5 6V4.3h5V6M7.2 6l.8 13a1.8 1.8 0 0 0 1.8 1.7h4.4a1.8 1.8 0 0 0 1.8-1.7l.8-13M10.2 9.5v8M13.8 9.5v8" /></svg>;
    if (kind === "note") return <svg {...common}><path d="M4.5 4.5h15v11h-8.5L7 19.5v-4H4.5z" /><path d="M7.5 8.5h9M7.5 11.5h6" /></svg>;
    if (kind === "bookmark") return <svg {...common}><path d="M7 4h10a1 1 0 0 1 1 1v15l-6-3.2L6 20V5a1 1 0 0 1 1-1z" /></svg>;
    if (kind === "erase") return <svg {...common}><path d="m7 21-4.3-4.3c-1-1-1-2.5 0-3.4l9.6-9.6c1-1 2.5-1 3.4 0l5.6 5.6c1 1 2.5 1 3.4 0l-8.3 8.4" /><path d="M22 21H7" /><path d="m5 11 9 9" /></svg>;
    if (kind === "pen") return <svg {...common}><path d="m14 5 5 5-8.5 8.5a2 2 0 0 1-2.8 0l-2.2-2.2a2 2 0 0 1 0-2.8z" /><path d="m8.5 10.5 5 5" /></svg>;
    return <svg {...common}><rect x="3" y="14" width="11" height="3.6" rx="1" fill="#ffe08a" stroke="none" /><path d="M14.2 12.8 18.8 8.2 21 10.4 16.4 15H14.2z" /></svg>;
}

type PptxMenuState = {
    x: number;
    y: number;
    point: PdfDrawingPoint;
    markId: string;
    noteId: string;
    bookmarkId: string;
};

function PptxMenu({
    lang,
    state,
    mark,
    note,
    bookmark,
    noteName,
    onRect,
    onAnnotate,
    onClearMark,
    onAddBookmark,
    onRenameBookmark,
    onDeleteBookmark,
    onRenameNote,
    onDeleteNote,
    onToggleDraw,
    drawActive,
    onClose,
}: {
    lang: string;
    state: PptxMenuState;
    mark: PdfMark | null;
    note: PdfTranslation | null;
    bookmark: PdfBookmark | null;
    noteName: string;
    onRect: (color: PdfMarkColor) => void;
    onAnnotate: (body: string) => void;
    onClearMark: () => void;
    onAddBookmark: (name: string, color: PdfBookmarkColor) => void;
    onRenameBookmark: (name: string, color: PdfBookmarkColor) => void;
    onDeleteBookmark: () => void;
    onRenameNote: (name: string) => void;
    onDeleteNote: () => void;
    onToggleDraw: () => void;
    drawActive: boolean;
    onClose: () => void;
}) {
    const say = (zh: string) => say2(lang, zh);
    const rootRef = useRef<HTMLDivElement>(null);
    const [place, setPlace] = useState({ left: state.x, top: state.y });
    const [noteEditor, setNoteEditor] = useState(false);
    const [noteText, setNoteText] = useState("");
    const [bookmarkEditor, setBookmarkEditor] = useState<"" | "add" | "rename">("");
    const [bookmarkDraft, setBookmarkDraft] = useState("");
    const [bookmarkColor, setBookmarkColor] = useState<PdfBookmarkColor>("yellow");
    const [nameEditor, setNameEditor] = useState(false);
    const [nameDraft, setNameDraft] = useState("");
    const editorOpen = noteEditor || bookmarkEditor !== "" || nameEditor;
    const imeKey = (event: React.KeyboardEvent) => event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229;

    useLayoutEffect(() => {
        const root = rootRef.current;
        if (!root) return;
        const box = root.getBoundingClientRect();
        const width = box.width || root.offsetWidth;
        const height = box.height || root.offsetHeight;
        const maxLeft = Math.max(8, window.innerWidth - width - 8);
        const maxTop = Math.max(8, window.innerHeight - height - 8);
        setPlace({ left: Math.min(Math.max(8, state.x), maxLeft), top: Math.min(Math.max(8, state.y), maxTop) });
    }, [state.x, state.y, editorOpen]);

    useEffect(() => {
        const onPointer = (event: MouseEvent) => {
            const target = event.target as Node | null;
            if (target && rootRef.current?.contains(target)) return;
            onClose();
        };
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== "Escape" || editorOpen) return;
            event.preventDefault();
            event.stopPropagation();
            onClose();
        };
        window.addEventListener("mousedown", onPointer);
        window.addEventListener("keydown", onKey, true);
        return () => {
            window.removeEventListener("mousedown", onPointer);
            window.removeEventListener("keydown", onKey, true);
        };
    }, [onClose, editorOpen]);

    const pressHandledRef = useRef<EventTarget | null>(null);
    const activate = (action: () => void) => ({
        onMouseDown: (event: React.MouseEvent) => {
            if (event.button !== 0) return;
            event.preventDefault();
            event.stopPropagation();
            pressHandledRef.current = event.currentTarget;
            action();
        },
        onClick: (event: React.MouseEvent) => {
            if (pressHandledRef.current === event.currentTarget) {
                pressHandledRef.current = null;
                return;
            }
            event.preventDefault();
            action();
        },
    });

    const submitNote = () => {
        const body = clipPdfAnnotationBody(noteText);
        if (!body) return;
        onAnnotate(body);
    };
    const submitBookmark = () => {
        const name = bookmarkDraft.trim();
        if (!name) return;
        if (bookmarkEditor === "rename") onRenameBookmark(name, bookmarkColor);
        else onAddBookmark(name, bookmarkColor);
    };
    const submitName = () => {
        const name = clipPdfAnnotationName(nameDraft);
        if (!name) return;
        onRenameNote(name);
    };

    return (
        <div
            ref={rootRef}
            className="companion-pdf-menu"
            data-testid="pptx-menu"
            role="menu"
            style={{ left: place.left, top: place.top, position: "fixed", zIndex: 30 } as CSSProperties}
            onMouseDown={(event) => event.stopPropagation()}
            onContextMenu={(event) => {
                event.preventDefault();
                event.stopPropagation();
            }}
        >
            {mark ? (
                <div className="companion-pdf-menu-group">
                    <button type="button" role="menuitem" data-testid="pptx-clear-mark" className="companion-pdf-menu-item companion-pdf-menu-item--danger" {...activate(onClearMark)}>
                        <PptxMenuGlyph kind="erase" />
                        <span className="companion-pdf-menu-item-label">{say("取消高亮")}</span>
                    </button>
                </div>
            ) : null}
            {note ? (
                <div className="companion-pdf-menu-group">
                    {nameEditor ? (
                        <div className="companion-pdf-bookmark-editor">
                            <input
                                data-testid="pptx-note-name"
                                value={nameDraft}
                                autoFocus
                                maxLength={40}
                                placeholder={say("批注名")}
                                aria-label={say("批注名")}
                                onChange={(event) => setNameDraft(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === "Escape") {
                                        if (imeKey(event)) { event.stopPropagation(); return; }
                                        event.preventDefault();
                                        event.stopPropagation();
                                        setNameEditor(false);
                                        return;
                                    }
                                    if (event.key === "Enter" && !imeKey(event)) {
                                        event.preventDefault();
                                        submitName();
                                    }
                                }}
                            />
                            <div className="companion-pdf-bookmark-editor-actions">
                                <button type="button" className="companion-pdf-menu-btn" onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => setNameEditor(false)}>{say("取消")}</button>
                                <button type="button" className="companion-pdf-menu-btn companion-pdf-menu-btn--accent" data-testid="pptx-note-name-save" disabled={!nameDraft.trim()} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={submitName}>{say("确定")}</button>
                            </div>
                        </div>
                    ) : (
                        <>
                            <button type="button" role="menuitem" data-testid="pptx-note-rename" className="companion-pdf-menu-item" {...activate(() => { setNameDraft(noteName); setNameEditor(true); })}>
                                <PptxMenuGlyph kind="pen" />
                                <span className="companion-pdf-menu-item-label">{say("批注改名")}</span>
                            </button>
                            <button type="button" role="menuitem" data-testid="pptx-note-delete" className="companion-pdf-menu-item companion-pdf-menu-item--danger" {...activate(onDeleteNote)}>
                                <PptxMenuGlyph kind="trash" />
                                <span className="companion-pdf-menu-item-label">{say("删除批注")}</span>
                            </button>
                        </>
                    )}
                </div>
            ) : null}
            {bookmark ? (
                <div className="companion-pdf-menu-group">
                    {bookmarkEditor === "rename" ? (
                        <div className="companion-pdf-bookmark-editor">
                            <input
                                data-testid="pptx-bookmark-name"
                                value={bookmarkDraft}
                                autoFocus
                                maxLength={40}
                                placeholder={say("书签名")}
                                aria-label={say("书签名")}
                                onChange={(event) => setBookmarkDraft(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === "Escape") {
                                        if (imeKey(event)) { event.stopPropagation(); return; }
                                        event.preventDefault();
                                        event.stopPropagation();
                                        setBookmarkEditor("");
                                        return;
                                    }
                                    if (event.key === "Enter" && !imeKey(event)) {
                                        event.preventDefault();
                                        submitBookmark();
                                    }
                                }}
                            />
                            <div className="companion-pdf-menu-colors companion-pdf-menu-colors--bookmarks" role="group" aria-label={say("书签颜色")}>
                                {PDF_BOOKMARK_COLORS.map((color) => (
                                    <button key={color.id} type="button" className="companion-pdf-menu-color" aria-pressed={bookmarkColor === color.id} aria-label={say(`${color.label}书签`)} title={say(`${color.label}书签`)} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => setBookmarkColor(color.id)}>
                                        <span style={{ background: color.fill }} />
                                    </button>
                                ))}
                            </div>
                            <div className="companion-pdf-bookmark-editor-actions">
                                <button type="button" className="companion-pdf-menu-btn" onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => setBookmarkEditor("")}>{say("取消")}</button>
                                <button type="button" className="companion-pdf-menu-btn companion-pdf-menu-btn--accent" data-testid="pptx-bookmark-save" disabled={!bookmarkDraft.trim()} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={submitBookmark}>{say("确定")}</button>
                            </div>
                        </div>
                    ) : (
                        <>
                            <button type="button" role="menuitem" data-testid="pptx-bookmark-rename" className="companion-pdf-menu-item" {...activate(() => { setBookmarkDraft(bookmark.name); setBookmarkColor(bookmark.color); setBookmarkEditor("rename"); })}>
                                <PptxMenuGlyph kind="pen" />
                                <span className="companion-pdf-menu-item-label">{say("重命名书签")}</span>
                            </button>
                            <button type="button" role="menuitem" data-testid="pptx-bookmark-delete" className="companion-pdf-menu-item companion-pdf-menu-item--danger" {...activate(onDeleteBookmark)}>
                                <PptxMenuGlyph kind="trash" />
                                <span className="companion-pdf-menu-item-label">{say("删除书签")}</span>
                            </button>
                        </>
                    )}
                </div>
            ) : null}
            {!mark && !note && !bookmark ? (
                <>
                    <div className="companion-pdf-menu-group">
                        <p className="companion-pdf-menu-title"><PptxMenuGlyph kind="highlight" />{say("高亮框选")}</p>
                        <div className="companion-pdf-menu-colors" role="group" aria-label={say("高亮颜色")}>
                            {PDF_MARK_COLORS.map((color) => (
                                <button
                                    key={color.id}
                                    type="button"
                                    role="menuitem"
                                    data-testid={`pptx-rect-${color.id}`}
                                    className="companion-pdf-menu-color"
                                    aria-label={say(`${color.label}高亮`)}
                                    title={say(`${color.label}高亮`)}
                                    {...activate(() => onRect(color.id))}
                                >
                                    <span style={{ background: color.fill }} />
                                </button>
                            ))}
                        </div>
                    </div>
                    <div className="companion-pdf-menu-group">
                        {noteEditor ? (
                            <div data-testid="pptx-annotation-editor" className="companion-pdf-menu-editor">
                                <textarea
                                    className="companion-pdf-menu-field"
                                    data-testid="pptx-annotation-input"
                                    value={noteText}
                                    autoFocus
                                    rows={3}
                                    maxLength={MAX_MENU_BODY}
                                    placeholder={say("写批注…")}
                                    aria-label={say("写批注…")}
                                    onChange={(event) => setNoteText(event.target.value)}
                                    onKeyDown={(event) => {
                                        if (event.key === "Escape") {
                                            if (imeKey(event)) { event.stopPropagation(); return; }
                                            event.preventDefault();
                                            event.stopPropagation();
                                            setNoteEditor(false);
                                            setNoteText("");
                                            return;
                                        }
                                        if (event.key === "Enter" && !event.shiftKey && !imeKey(event)) {
                                            event.preventDefault();
                                            submitNote();
                                        }
                                    }}
                                />
                                <div className="companion-pdf-menu-editor-actions">
                                    <button type="button" className="companion-pdf-menu-btn" onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => { setNoteEditor(false); setNoteText(""); }}>{say("取消")}</button>
                                    <button type="button" className="companion-pdf-menu-btn companion-pdf-menu-btn--accent" data-testid="pptx-annotation-save" disabled={!noteText.trim()} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={submitNote}>{say("保存批注")}</button>
                                </div>
                            </div>
                        ) : (
                            <button type="button" role="menuitem" data-testid="pptx-menu-annotate" className="companion-pdf-menu-item" {...activate(() => setNoteEditor(true))}>
                                <PptxMenuGlyph kind="note" />
                                <span className="companion-pdf-menu-item-label">{say("批注")}</span>
                            </button>
                        )}
                    </div>
                    <div className="companion-pdf-menu-group">
                        {bookmarkEditor === "add" ? (
                            <div className="companion-pdf-bookmark-editor">
                                <input
                                    data-testid="pptx-bookmark-name"
                                    value={bookmarkDraft}
                                    autoFocus
                                    maxLength={40}
                                    placeholder={say("书签名")}
                                    aria-label={say("书签名")}
                                    onChange={(event) => setBookmarkDraft(event.target.value)}
                                    onKeyDown={(event) => {
                                        if (event.key === "Escape") {
                                            if (imeKey(event)) { event.stopPropagation(); return; }
                                            event.preventDefault();
                                            event.stopPropagation();
                                            setBookmarkEditor("");
                                            return;
                                        }
                                        if (event.key === "Enter" && !imeKey(event)) {
                                            event.preventDefault();
                                            submitBookmark();
                                        }
                                    }}
                                />
                                <div className="companion-pdf-menu-colors companion-pdf-menu-colors--bookmarks" role="group" aria-label={say("书签颜色")}>
                                    {PDF_BOOKMARK_COLORS.map((color) => (
                                        <button key={color.id} type="button" className="companion-pdf-menu-color" aria-pressed={bookmarkColor === color.id} aria-label={say(`${color.label}书签`)} title={say(`${color.label}书签`)} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => setBookmarkColor(color.id)}>
                                            <span style={{ background: color.fill }} />
                                        </button>
                                    ))}
                                </div>
                                <div className="companion-pdf-bookmark-editor-actions">
                                    <button type="button" className="companion-pdf-menu-btn" onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={() => setBookmarkEditor("")}>{say("取消")}</button>
                                    <button type="button" className="companion-pdf-menu-btn companion-pdf-menu-btn--accent" data-testid="pptx-bookmark-save" disabled={!bookmarkDraft.trim()} onMouseDown={(event) => { event.preventDefault(); event.stopPropagation(); }} onClick={submitBookmark}>{say("确定")}</button>
                                </div>
                            </div>
                        ) : (
                            <button type="button" role="menuitem" data-testid="pptx-bookmark-add" className="companion-pdf-menu-item" {...activate(() => { setBookmarkDraft(""); setBookmarkColor("yellow"); setBookmarkEditor("add"); })}>
                                <PptxMenuGlyph kind="bookmark" />
                                <span className="companion-pdf-menu-item-label">{say("加书签")}</span>
                            </button>
                        )}
                    </div>
                    <div className="companion-pdf-menu-group">
                        <button type="button" role="menuitem" data-testid="pptx-menu-draw" className="companion-pdf-menu-item" aria-pressed={drawActive} {...activate(onToggleDraw)}>
                            <PptxMenuGlyph kind="pen" />
                            <span className="companion-pdf-menu-item-label">{drawActive ? say("退出手绘") : say("手绘")}</span>
                        </button>
                    </div>
                </>
            ) : null}
        </div>
    );
}

export function CompanionPptxAnnotator({
    path,
    page,
    lang,
    onJumpSlide,
    children,
}: {
    /** The deck's own path — the store and share key. */
    path: string;
    /** Current slide, 1-based. Marks/drawings/notes/bookmarks store this page. */
    page: number;
    lang?: string;
    /** Rail jumps pick a slide. Receives the 1-based page. */
    onJumpSlide: (page: number) => void;
    children: ReactNode;
}) {
    const say = (zh: string) => say2(lang || "", zh);
    const stageRef = useRef<HTMLDivElement>(null);
    const [marks, setMarks] = useState<PdfMark[]>(() => loadPdfMarks(path));
    const [drawings, setDrawings] = useState<PdfDrawing[]>(() => loadPdfDrawings(path));
    const [bookmarks, setBookmarks] = useState<PdfBookmark[]>(() => loadPdfBookmarks(path));
    const [notes, setNotes] = useState<PdfTranslation[]>(() => loadPdfAnnotations(path));
    const [menu, setMenu] = useState<PptxMenuState | null>(null);
    const [drawMode, setDrawMode] = useState(false);
    const [drawTool, setDrawTool] = useState<CompanionDrawTool>("line");
    const [drawColor, setDrawColor] = useState<PdfMarkColor>("yellow");
    const [rectColor, setRectColor] = useState<PdfMarkColor | null>(null);
    const [rectDraft, setRectDraft] = useState<PdfMarkRect | null>(null);
    const [openNoteId, setOpenNoteId] = useState("");
    const marksRef = useRef(marks);
    marksRef.current = marks;
    const drawingsRef = useRef(drawings);
    drawingsRef.current = drawings;
    const bookmarksRef = useRef(bookmarks);
    bookmarksRef.current = bookmarks;
    const notesRef = useRef(notes);
    notesRef.current = notes;
    const menuRef = useRef(menu);
    menuRef.current = menu;
    const drawToolRef = useRef<CompanionDrawTool>("line");
    const pathKey = pdfMarkPathKey(path);

    // A slide switch closes any open menu — its anchor point was on the old
    // page. The armed rect color and pen mode stay: they apply to whatever
    // slide is showing when the drag begins.
    useEffect(() => {
        setMenu(null);
        setRectDraft(null);
    }, [page]);

    useEffect(() => {
        setMarks(loadPdfMarks(path));
        setDrawings(loadPdfDrawings(path));
        setBookmarks(loadPdfBookmarks(path));
        setNotes(loadPdfAnnotations(path));
        setMenu(null);
        setOpenNoteId("");
        setRectDraft(null);
        setRectColor(null);
        setDrawMode(false);
    }, [path]);

    useEffect(() => {
        const offMarks = subscribePdfMarks((key) => {
            if (key === pathKey) setMarks(loadPdfMarks(path));
        });
        const offDrawings = subscribePdfDrawings((key) => {
            if (key === pathKey) setDrawings(loadPdfDrawings(path));
        });
        const offBookmarks = subscribePdfBookmarks((key) => {
            if (key === pathKey) setBookmarks(loadPdfBookmarks(path));
        });
        const offNotes = subscribePdfTranslations((key) => {
            if (key === pathKey) setNotes(loadPdfAnnotations(path));
        });
        const offReader = subscribeCompanionReaderState(() => {
            setMarks(loadPdfMarks(path));
            setDrawings(loadPdfDrawings(path));
            setBookmarks(loadPdfBookmarks(path));
            setNotes(loadPdfAnnotations(path));
        });
        return () => {
            offMarks();
            offDrawings();
            offBookmarks();
            offNotes();
            offReader();
        };
    }, [path, pathKey]);

    const writeMarks = (next: PdfMark[]) => setMarks(savePdfMarks(path, next, true));
    const writeDrawings = (next: PdfDrawing[]) => setDrawings(savePdfDrawings(path, next, true));
    const writeBookmarks = (next: PdfBookmark[]) => setBookmarks(savePdfBookmarks(path, next, true));
    const writeNotes = (next: PdfTranslation[], announce: boolean) => {
        const saved = savePdfTranslations(path, [
            ...loadPdfTranslations(path).filter((item) => item.kind !== "annotation"),
            ...next,
        ]);
        setNotes(saved.filter((item) => item.kind === "annotation"));
        if (announce) notifyPdfAnnotationsChanged(path);
    };

    const addDrawing = (drawing: PdfDrawing) => writeDrawings([...drawingsRef.current, drawing]);
    const eraseDrawings = (ids: string[]) => {
        const doomed = new Set(ids);
        writeDrawings(drawingsRef.current.filter((drawing) => !doomed.has(drawing.id)));
    };
    const undoDrawing = () => {
        for (let index = drawingsRef.current.length - 1; index >= 0; index -= 1) {
            const drawing = drawingsRef.current[index];
            if (drawing.shared || drawing.page !== page) continue;
            writeDrawings([...drawingsRef.current.slice(0, index), ...drawingsRef.current.slice(index + 1)]);
            return;
        }
    };
    const selectDrawTool = (next: CompanionDrawTool) => {
        if (next !== "eraser") drawToolRef.current = next;
        setDrawTool(next);
    };
    const toggleDrawMode = () => {
        setMenu(null);
        setRectColor(null);
        setRectDraft(null);
        setDrawMode((current) => !current);
    };

    const beginRect = (color: PdfMarkColor) => {
        setDrawMode(false);
        setRectColor(color);
        setRectDraft(null);
        setMenu(null);
    };

    const createNote = (body: string) => {
        const current = menuRef.current;
        const clipped = clipPdfAnnotationBody(body);
        if (!current || !clipped) return;
        const anchor = clampAnchor(current.point);
        const note: PdfTranslation = {
            id: newPdfTranslationId(),
            kind: "annotation",
            text: say(`第${page}页`),
            translation: clipped,
            parts: [{ page, rects: [anchor] }],
            place: { x: current.point.x, y: current.point.y },
        };
        writeNotes([...notesRef.current, note], true);
        setOpenNoteId(note.id);
        setMenu(null);
    };

    const renameNote = (name: string) => {
        const id = menuRef.current?.noteId || "";
        const clipped = clipPdfAnnotationName(name);
        if (!id || !clipped) return;
        writeNotes(notesRef.current.map((item) => (item.id === id ? { ...item, name: clipped } : item)), true);
        setMenu(null);
    };

    const deleteNote = () => {
        const id = menuRef.current?.noteId || "";
        if (!id) return;
        writeNotes(notesRef.current.filter((item) => item.id !== id), true);
        if (openNoteId === id) setOpenNoteId("");
        setMenu(null);
    };

    const addBookmark = (name: string, color: PdfBookmarkColor) => {
        const current = menuRef.current;
        const trimmed = name.replace(/\s+/g, " ").trim();
        if (!current || !trimmed) return;
        const bookmark: PdfBookmark = {
            id: newPdfBookmarkId(),
            page,
            x: current.point.x,
            y: current.point.y,
            color,
            name: Array.from(trimmed).slice(0, 40).join(""),
        };
        writeBookmarks([...bookmarksRef.current, bookmark]);
        setMenu(null);
    };

    const renameBookmark = (name: string, color: PdfBookmarkColor) => {
        const id = menuRef.current?.bookmarkId || "";
        const trimmed = name.replace(/\s+/g, " ").trim();
        if (!id || !trimmed) return;
        writeBookmarks(bookmarksRef.current.map((item) => (item.id === id ? { ...item, name: trimmed.slice(0, 40), color } : item)));
        setMenu(null);
    };

    const deleteBookmark = () => {
        const id = menuRef.current?.bookmarkId || "";
        if (!id) return;
        writeBookmarks(bookmarksRef.current.filter((item) => item.id !== id));
        setMenu(null);
    };

    const clearMark = () => {
        const id = menuRef.current?.markId || "";
        if (!id) return;
        writeMarks(marksRef.current.filter((mark) => mark.id !== id));
        setMenu(null);
    };

    const updateNoteBody = (id: string, body: string) => {
        const clipped = clipPdfAnnotationBody(body);
        const note = notesRef.current.find((item) => item.id === id);
        if (!note || !clipped || clipped === note.translation) return;
        writeNotes(notesRef.current.map((item) => (item.id === id ? { ...item, translation: clipped } : item)), true);
    };

    const updateNotePlace = (id: string, place: { x: number; y: number }) => {
        writeNotes(notesRef.current.map((item) => (item.id === id ? { ...item, place: { x: place.x, y: place.y } } : item)), true);
    };

    // The rect-highlight gesture is a drag on a transparent layer, like the
    // pen's box tool but committing a stored mark instead of a drawing.
    const startRect = (event: React.MouseEvent) => {
        if (!rectColor || event.button !== 0) return;
        // A rect drag that begins on a pin, ribbon, or the draw bar is a click
        // on that control, not a marquee start.
        const target = event.target as HTMLElement | null;
        if (target?.closest?.("[data-pptx-note], [data-pptx-bookmark], .companion-doc-draw, .companion-pptx-note-card")) return;
        event.preventDefault();
        const start = pointOf(stageRef.current, event.clientX, event.clientY);
        if (!start) return;
        let draft: PdfMarkRect = { x: start.x, y: start.y, w: 0, h: 0 };
        setRectDraft(draft);
        const move = (moveEvent: MouseEvent) => {
            const next = pointOf(stageRef.current, moveEvent.clientX, moveEvent.clientY);
            if (!next) return;
            draft = {
                x: Math.min(start.x, next.x),
                y: Math.min(start.y, next.y),
                w: Math.abs(next.x - start.x),
                h: Math.abs(next.y - start.y),
            };
            setRectDraft(draft);
        };
        const finish = (upEvent: Event) => {
            window.removeEventListener("mousemove", move, true);
            window.removeEventListener("mouseup", finish, true);
            window.removeEventListener("blur", finish);
            // The release point counts even when no move event fired in
            // between (a quick click-drag commits the same rect).
            if (upEvent instanceof MouseEvent) {
                const end = pointOf(stageRef.current, upEvent.clientX, upEvent.clientY);
                if (end && Number.isFinite(end.x) && Number.isFinite(end.y)) {
                    draft = {
                        x: Math.min(start.x, end.x),
                        y: Math.min(start.y, end.y),
                        w: Math.abs(end.x - start.x),
                        h: Math.abs(end.y - start.y),
                    };
                }
            }
            setRectDraft(null);
            setRectColor(null);
            if (draft.w < 0.004 || draft.h < 0.004) return;
            const mark: PdfMark = {
                id: newPdfMarkId(),
                page,
                color: rectColor,
                rects: [draft],
                text: "",
            };
            writeMarks([...marksRef.current, mark]);
        };
        window.addEventListener("mousemove", move, true);
        window.addEventListener("mouseup", finish, true);
        window.addEventListener("blur", finish);
    };

    const onStageContextMenu = (event: React.MouseEvent) => {
        const stage = stageRef.current;
        if (!stage) return;
        const target = event.target as HTMLElement | null;
        const pin = target?.closest?.("[data-pptx-note]");
        if (pin && stage.contains(pin)) {
            const id = pin.getAttribute("data-pptx-note") || "";
            const point = pointOf(stage, event.clientX, event.clientY);
            if (!point) return;
            event.preventDefault();
            setMenu({ x: event.clientX, y: event.clientY, point, markId: "", noteId: id, bookmarkId: "" });
            return;
        }
        const ribbon = target?.closest?.("[data-pptx-bookmark]");
        if (ribbon && stage.contains(ribbon)) {
            const id = ribbon.getAttribute("data-pptx-bookmark") || "";
            const point = pointOf(stage, event.clientX, event.clientY);
            if (!point) return;
            event.preventDefault();
            setMenu({ x: event.clientX, y: event.clientY, point, markId: "", noteId: "", bookmarkId: id });
            return;
        }
        const point = pointOf(stage, event.clientX, event.clientY);
        if (!point) return;
        event.preventDefault();
        const hit = markAt(marksRef.current, page, point);
        setMenu({ x: event.clientX, y: event.clientY, point, markId: hit?.id ?? "", noteId: "", bookmarkId: "" });
    };

    const pageMarks = marks.filter((mark) => mark.page === page);
    const pageDrawings = drawings.filter((drawing) => drawing.page === page);
    const pageBookmarks = bookmarks.filter((bookmark) => bookmark.page === page);
    const pageNotes = notes.filter((note) => note.parts.some((part) => part.page === page));
    const menuMark = menu?.markId ? marks.find((mark) => mark.id === menu.markId) ?? null : null;
    const menuNote = menu?.noteId ? notes.find((note) => note.id === menu.noteId) ?? null : null;
    const menuBookmark = menu?.bookmarkId ? bookmarks.find((bookmark) => bookmark.id === menu.bookmarkId) ?? null : null;

    const railBookmarks: AnnotatorRailItem[] = bookmarks.map((bookmark) => ({
        id: bookmark.id,
        label: bookmark.name,
        page: String(bookmark.page),
        swatch: (
            <svg viewBox="0 0 16 40" aria-hidden="true" focusable="false">
                <path d="M.8.8h14.4V28L8 38.5.8 28Z" fill={pdfBookmarkFill(bookmark.color)} stroke={pdfBookmarkInk(bookmark.color)} strokeWidth="1.2" strokeLinejoin="round" />
            </svg>
        ),
        here: bookmark.page === page,
        onJump: () => onJumpSlide(bookmark.page),
    }));
    const railNotes: AnnotatorRailItem[] = notes.map((note) => ({
        id: note.id,
        label: note.name || note.text,
        page: String(note.parts[0]?.page ?? ""),
        here: note.parts.some((part) => part.page === page),
        onJump: () => {
            const target = note.parts[0]?.page;
            if (target) onJumpSlide(target);
            setOpenNoteId(note.id);
        },
    }));

    return (
        <div className="companion-doc-annotator" data-testid="pptx-annotator" style={{ display: "flex", alignItems: "center", maxWidth: "100%", maxHeight: "100%", minWidth: 0, minHeight: 0 }}>
            {bookmarks.length + notes.length > 0 ? (
                <CompanionAnnotatorRail lang={lang} bookmarks={railBookmarks} notes={railNotes} testId="pptx-annotator-rail" />
            ) : null}
            <div
                ref={stageRef}
                className="companion-pptx-annotator"
                data-testid="pptx-annotator-stage"
                onContextMenu={onStageContextMenu}
                onMouseDown={rectColor ? startRect : undefined}
                style={rectColor ? { cursor: "crosshair" } : undefined}
            >
                {children}
                {pageMarks.map((mark) => mark.rects.map((rect, index) => (
                    <span
                        key={`${mark.id}:${index}`}
                        className="companion-pptx-mark"
                        data-testid={`pptx-mark-${mark.id}`}
                        style={{
                            left: `${rect.x * 100}%`,
                            top: `${rect.y * 100}%`,
                            width: `${rect.w * 100}%`,
                            height: `${rect.h * 100}%`,
                            background: pdfMarkFill(mark.color),
                            opacity: 0.42,
                            borderColor: pdfMarkFill(mark.color),
                        }}
                    />
                )))}
                {rectDraft ? (
                    <span
                        className="companion-pptx-mark companion-pptx-mark--draft"
                        data-testid="pptx-mark-draft"
                        style={{
                            left: `${rectDraft.x * 100}%`,
                            top: `${rectDraft.y * 100}%`,
                            width: `${rectDraft.w * 100}%`,
                            height: `${rectDraft.h * 100}%`,
                            background: rectColor ? pdfMarkFill(rectColor) : "transparent",
                            opacity: 0.35,
                            borderColor: rectColor ? COMPANION_DRAWING_INK[rectColor] : "transparent",
                        }}
                    />
                ) : null}
                <CompanionDrawingLayer
                    page={page}
                    drawings={pageDrawings}
                    active={drawMode}
                    color={drawColor}
                    tool={drawTool}
                    onAdd={addDrawing}
                    onErase={eraseDrawings}
                    testId="pptx-drawing-layer"
                />
                {pageBookmarks.map((bookmark) => (
                    <button
                        key={bookmark.id}
                        type="button"
                        className="companion-pptx-bookmark-tick"
                        data-pptx-bookmark={bookmark.id}
                        data-testid={`pptx-bookmark-tick-${bookmark.id}`}
                        title={bookmark.name}
                        aria-label={`${say("书签")}：${bookmark.name}`}
                        style={{
                            top: `${bookmark.y * 100}%`,
                            right: 0,
                            width: PDF_BOOKMARK_TICK_WIDTH_PX,
                            height: PDF_BOOKMARK_TICK_PX,
                            transform: "translateY(-50%)",
                        }}
                    >
                        <svg viewBox="0 0 16 40" aria-hidden="true" focusable="false">
                            <path d="M.8.8h14.4V28L8 38.5.8 28Z" fill={pdfBookmarkFill(bookmark.color)} stroke={pdfBookmarkInk(bookmark.color)} strokeWidth="1.2" strokeLinejoin="round" />
                            <path d="M2.15 1.8h3.15V26.6H2.15Z" fill="#fff" opacity=".5" />
                            <path d="M8 2.2v26" stroke={pdfBookmarkInk(bookmark.color)} strokeWidth=".7" opacity=".4" />
                        </svg>
                    </button>
                ))}
                {pageNotes.map((note) => {
                    const part = note.parts.find((item) => item.page === page);
                    const anchor = part?.rects[0];
                    if (!anchor) return null;
                    const open = openNoteId === note.id;
                    const placeX = note.place?.x ?? anchor.x + anchor.w;
                    const placeY = note.place?.y ?? anchor.y;
                    return (
                        <div key={note.id}>
                            <button
                                type="button"
                                className="companion-pptx-note-pin"
                                data-pptx-note={note.id}
                                data-testid={`pptx-note-pin-${note.id}`}
                                aria-label={say("批注")}
                                aria-expanded={open}
                                title={note.name || note.text}
                                style={{
                                    left: `${(anchor.x + anchor.w) * 100}%`,
                                    top: `${anchor.y * 100}%`,
                                }}
                                onClick={(event) => {
                                    event.preventDefault();
                                    event.stopPropagation();
                                    setOpenNoteId(open ? "" : note.id);
                                }}
                            >
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                    <path d="M4.5 4.5h15v11h-8.5L7 19.5v-4H4.5z" />
                                    <path d="M7.5 8.5h9" />
                                    <path d="M7.5 11.5h6" />
                                </svg>
                            </button>
                            {open ? (
                                <PptxNoteCard
                                    lang={lang || ""}
                                    note={note}
                                    stageRef={stageRef}
                                    notePlaceX={placeX}
                                    notePlaceY={placeY}
                                    onBody={updateNoteBody}
                                    onPlace={updateNotePlace}
                                    onClose={() => setOpenNoteId("")}
                                />
                            ) : null}
                        </div>
                    );
                })}
                <div className="companion-doc-draw" data-testid="pptx-draw" onContextMenu={(event) => { event.preventDefault(); event.stopPropagation(); }} style={{ position: "absolute", top: 8, left: 8, right: "auto", justifyContent: "flex-start", height: "auto", paddingRight: 0 }}>
                    <button
                        type="button"
                        className="companion-pdf-draw-toggle"
                        data-testid="pptx-draw-toggle"
                        aria-label={say("手绘")}
                        aria-pressed={drawMode}
                        title={drawMode ? say("退出手绘") : say("手绘 — 在幻灯片上画线或框")}
                        onClick={toggleDrawMode}
                    >
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                            <path d="M4 20h4L18.5 9.5a2.5 2.5 0 0 0-3.5-3.5L4 16.5V20Z" />
                            <path d="m13.5 7.5 3 3" />
                        </svg>
                    </button>
                    {drawMode ? (
                        <div className="companion-pdf-draw-panel" data-testid="pptx-draw-panel">
                            <div className="companion-pdf-draw-colors" role="group" aria-label={say("手绘颜色")}>
                                {PDF_MARK_COLORS.map((option) => (
                                    <button
                                        key={option.id}
                                        type="button"
                                        className="companion-pdf-menu-color"
                                        data-testid={`pptx-draw-color-${option.id}`}
                                        aria-label={say(`${option.label}画笔`)}
                                        aria-pressed={drawColor === option.id}
                                        disabled={drawTool === "eraser"}
                                        title={say(`${option.label}画笔`)}
                                        onClick={() => setDrawColor(option.id)}
                                    >
                                        <span style={{ background: COMPANION_DRAWING_INK[option.id] }} />
                                    </button>
                                ))}
                            </div>
                            <span className="companion-pdf-draw-sep" aria-hidden="true" />
                            <div className="companion-pdf-draw-tools" role="group" aria-label={say("手绘工具")}>
                                <button type="button" data-testid="pptx-draw-line" aria-label={say("画线")} aria-pressed={drawTool === "line"} title={say("画线")} onClick={() => selectDrawTool("line")}>
                                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M5 19 19 5" /><circle cx="5" cy="19" r="1.5" fill="currentColor" stroke="none" /><circle cx="19" cy="5" r="1.5" fill="currentColor" stroke="none" /></svg>
                                </button>
                                <button type="button" data-testid="pptx-draw-box" aria-label={say("画框")} aria-pressed={drawTool === "box"} title={say("画框")} onClick={() => selectDrawTool("box")}>
                                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="4" y="6" width="16" height="12" rx="1.5" /></svg>
                                </button>
                                <button type="button" data-testid="pptx-draw-eraser" aria-label={say("擦除")} aria-pressed={drawTool === "eraser"} title={say("擦除 — 圈选要清除的线或框")} onClick={() => selectDrawTool(drawTool === "eraser" ? drawToolRef.current : "eraser")}>
                                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m7 21-4.3-4.3a2.4 2.4 0 0 1 0-3.4l9.6-9.6a2.4 2.4 0 0 1 3.4 0l5.6 5.6a2.4 2.4 0 0 1 0 3.4L13 21" /><path d="M22 21H7" /><path d="m5 11 9 9" /></svg>
                                </button>
                            </div>
                            <span className="companion-pdf-draw-sep" aria-hidden="true" />
                            <div className="companion-pdf-draw-tools">
                                <button type="button" data-testid="pptx-draw-undo" aria-label={say("撤销上一笔")} disabled={!pageDrawings.some((item) => !item.shared)} title={say("撤销上一笔")} onClick={undoDrawing}>
                                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M9 7 4 12l5 5" /><path d="M4 12h9a5 5 0 0 1 5 5v1" /></svg>
                                </button>
                            </div>
                        </div>
                    ) : null}
                </div>
            </div>
            {menu ? (
                <PptxMenu
                    lang={lang || ""}
                    state={menu}
                    mark={menuMark}
                    note={menuNote}
                    bookmark={menuBookmark}
                    noteName={menuNote ? (menuNote.name || menuNote.text) : ""}
                    onRect={beginRect}
                    onAnnotate={createNote}
                    onClearMark={clearMark}
                    onAddBookmark={addBookmark}
                    onRenameBookmark={renameBookmark}
                    onDeleteBookmark={deleteBookmark}
                    onRenameNote={renameNote}
                    onDeleteNote={deleteNote}
                    onToggleDraw={toggleDrawMode}
                    drawActive={drawMode}
                    onClose={() => setMenu(null)}
                />
            ) : null}
        </div>
    );
}

function PptxNoteCard({
    lang,
    note,
    stageRef,
    notePlaceX,
    notePlaceY,
    onBody,
    onPlace,
    onClose,
}: {
    lang: string;
    note: PdfTranslation;
    stageRef: React.RefObject<HTMLDivElement | null>;
    notePlaceX: number;
    notePlaceY: number;
    onBody: (id: string, body: string) => void;
    onPlace: (id: string, place: { x: number; y: number }) => void;
    onClose: () => void;
}) {
    const say = (zh: string) => say2(lang, zh);
    const [editing, setEditing] = useState(false);
    const [draft, setDraft] = useState(note.translation);
    // A drag moves the card through local state; the store write happens once
    // on release instead of per pointer event.
    const [livePlace, setLivePlace] = useState<{ x: number; y: number } | null>(null);
    // The edge flip is pinned when the drag starts so the card does not jump
    // as its left edge crosses the midpoint under a steady cursor.
    const flipRef = useRef<{ x: boolean; y: boolean }>({ x: false, y: false });
    const cardRef = useRef<HTMLDivElement>(null);
    const imeKey = (event: React.KeyboardEvent) => event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229;
    const placeX = livePlace?.x ?? notePlaceX;
    const placeY = livePlace?.y ?? notePlaceY;

    const beginDrag = (event: React.PointerEvent) => {
        if (event.button !== 0) return;
        const stage = stageRef.current;
        const card = cardRef.current;
        if (!stage || !card) return;
        event.preventDefault();
        event.stopPropagation();
        const stageBox = stage.getBoundingClientRect();
        const cardBox = card.getBoundingClientRect();
        const offX = event.clientX - cardBox.left;
        const offY = event.clientY - cardBox.top;
        flipRef.current = { x: placeX > 0.55, y: placeY > 0.55 };
        const moved = (moveEvent: PointerEvent) => ({
            x: Math.min(1, Math.max(0, (moveEvent.clientX - stageBox.left - offX) / stageBox.width)),
            y: Math.min(1, Math.max(0, (moveEvent.clientY - stageBox.top - offY) / stageBox.height)),
        });
        let last = { x: placeX, y: placeY };
        const move = (moveEvent: PointerEvent) => {
            last = moved(moveEvent);
            setLivePlace(last);
        };
        const finish = (upEvent: PointerEvent) => {
            window.removeEventListener("pointermove", move, true);
            window.removeEventListener("pointerup", finish, true);
            window.removeEventListener("pointercancel", finish, true);
            if (upEvent.type === "pointerup") last = moved(upEvent);
            setLivePlace(null);
            onPlace(note.id, last);
        };
        window.addEventListener("pointermove", move, true);
        window.addEventListener("pointerup", finish, true);
        window.addEventListener("pointercancel", finish, true);
    };

    const submit = () => {
        const body = clipPdfAnnotationBody(draft);
        if (body) onBody(note.id, body);
        setEditing(false);
    };

    // Past half-way, flip the card so it opens over the slide instead of
    // clipping off the right/bottom edge.
    const flip = livePlace ? flipRef.current : { x: placeX > 0.55, y: placeY > 0.55 };
    const transform = `${flip.x ? "translateX(-100%)" : ""} ${flip.y ? "translateY(-100%)" : ""}`.trim() || undefined;

    return (
        <div
            ref={cardRef}
            className="companion-pptx-note-card"
            data-testid={`pptx-note-card-${note.id}`}
            style={{ left: `${placeX * 100}%`, top: `${placeY * 100}%`, transform }}
            onContextMenu={(event) => {
                // The head carries data-pptx-note, so a right-click there falls
                // through to the stage handler and opens the note's menu; the
                // body and editor keep the browser's menu suppressed.
                const head = (event.target as HTMLElement | null)?.closest?.(".companion-pptx-note-card-head");
                if (head) return;
                event.preventDefault();
                event.stopPropagation();
            }}
        >
            <div className="companion-pptx-note-card-head" data-pptx-note={note.id} onPointerDown={beginDrag}>
                <span className="companion-pptx-note-card-title">{note.name || note.text}</span>
                <button type="button" className="companion-pdf-menu-btn" data-testid={`pptx-note-edit-${note.id}`} onPointerDown={(event) => event.stopPropagation()} onClick={() => { setDraft(note.translation); setEditing(true); }}>{say("编辑")}</button>
                <button type="button" className="companion-pdf-menu-btn" data-testid={`pptx-note-close-${note.id}`} aria-label={say("关闭")} onPointerDown={(event) => event.stopPropagation()} onClick={onClose}>×</button>
            </div>
            {editing ? (
                <>
                    <textarea
                        data-testid={`pptx-note-body-${note.id}`}
                        value={draft}
                        autoFocus
                        maxLength={MAX_MENU_BODY}
                        onChange={(event) => setDraft(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === "Escape") {
                                if (imeKey(event)) { event.stopPropagation(); return; }
                                event.preventDefault();
                                event.stopPropagation();
                                setEditing(false);
                            }
                        }}
                    />
                    <div className="companion-pptx-note-card-actions">
                        <button type="button" onClick={() => setEditing(false)}>{say("取消")}</button>
                        <button type="button" data-accent="true" data-testid={`pptx-note-save-${note.id}`} disabled={!draft.trim()} onClick={submit}>{say("保存")}</button>
                    </div>
                </>
            ) : (
                <div className="companion-pptx-note-card-body">{note.translation}</div>
            )}
        </div>
    );
}
