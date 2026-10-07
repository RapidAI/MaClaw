import { useEffect, useRef, useState } from "react";
import { GetPetCompanionTranscript } from "../../../wailsjs/go/main/App";
import { EventsOn } from "../../../wailsjs/runtime";
import "./PetCompanionSurface.css";

function wavSource(payload: unknown): string {
    const raw = typeof payload === "string" ? payload.trim() : "";
    if (!raw) return "";
    if (raw.startsWith("data:")) return raw;
    return `data:audio/wav;base64,${raw}`;
}

function fadeOut(audio: HTMLAudioElement) {
    const start = audio.volume;
    const steps = 5;
    let step = 0;
    const timer = window.setInterval(() => {
        step += 1;
        audio.volume = Math.max(0, start * (1 - step / steps));
        if (step >= steps) {
            window.clearInterval(timer);
            audio.pause();
        }
    }, 20);
}

export function PetCompanionSurface() {
    const audioRef = useRef<HTMLAudioElement | null>(null);
    const [caption, setCaption] = useState("");
    const [transcriptOpen, setTranscriptOpen] = useState(false);
    const [transcript, setTranscript] = useState("");

    useEffect(() => {
        const onSpeech = (payload: unknown) => {
            const src = wavSource(payload);
            if (!src) return;
            if (audioRef.current) {
                audioRef.current.pause();
            }
            const audio = new Audio(src);
            audio.volume = 1;
            audioRef.current = audio;
            void audio.play().catch(() => {});
        };
        const onStop = () => {
            const audio = audioRef.current;
            if (!audio) return;
            fadeOut(audio);
        };
        const onBubble = (payload: unknown) => {
            const text = payload && typeof payload === "object" && "text" in payload
                ? String((payload as { text?: unknown }).text || "").trim()
                : "";
            setCaption(text);
        };
        const onOpen = () => {
            setTranscriptOpen(true);
            void GetPetCompanionTranscript()
                .then((text) => setTranscript(String(text || "")))
                .catch(() => setTranscript(""));
        };
        const offs = [
            EventsOn("pet-companion-speech", onSpeech),
            EventsOn("pet-companion-stop-speech", onStop),
            EventsOn("pet-companion-bubble", onBubble),
            EventsOn("open-pet-conversation", onOpen),
        ];
        return () => {
            offs.forEach((off) => {
                if (typeof off === "function") off();
            });
            audioRef.current?.pause();
        };
    }, []);

    useEffect(() => {
        if (!caption) return;
        const timer = window.setTimeout(() => setCaption(""), 4000);
        return () => window.clearTimeout(timer);
    }, [caption]);

    return (
        <>
            {caption ? (
                <div className="pet-companion-caption" role="status">{caption}</div>
            ) : null}
            {transcriptOpen ? (
                <div className="pet-companion-sheet" role="dialog" aria-label="宠物对话">
                    <div className="pet-companion-sheet-card">
                        <header>
                            <strong>宠物对话</strong>
                            <button type="button" onClick={() => setTranscriptOpen(false)}>关闭</button>
                        </header>
                        <pre>{transcript.trim() ? transcript : "还没有对话"}</pre>
                    </div>
                </div>
            ) : null}
        </>
    );
}
