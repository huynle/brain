import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";

/** One playback at a time; cancellation never starts a stale response. */
export function useAssistantSpeech(active: boolean) {
  const player = useRef<HTMLAudioElement | null>(null);
  const request = useRef<AbortController | null>(null);
  const cached = useRef<{ text: string; url: string } | null>(null);
  const generation = useRef(0);
  const [state, setState] = useState<"idle" | "loading" | "playing">("idle");
  const [error, setError] = useState("");
  const stop = () => {
    generation.current++;
    request.current?.abort();
    request.current = null;
    player.current?.pause();
    player.current = null;
    setState("idle");
  };
  useEffect(() => {
    if (!active) stop();
    return () => {
      generation.current++;
      request.current?.abort();
      player.current?.pause();
      if (cached.current) URL.revokeObjectURL(cached.current.url);
      cached.current = null;
    };
  }, [active]);
  const play = async (text: string) => {
    stop();
    const current = generation.current;
    setError("");
    if (!text.trim()) return;
    const controller = new AbortController();
    request.current = controller;
    setState("loading");
    try {
      if (cached.current?.text !== text) {
        const response = await api<Response>("/api/v1/assistant/speech", {
          method: "POST",
          body: { text },
          raw: true,
          signal: controller.signal,
        });
        const blob = await response.blob();
        if (current !== generation.current) return;
        if (cached.current) URL.revokeObjectURL(cached.current.url);
        cached.current = { text, url: URL.createObjectURL(blob) };
      }
      if (current !== generation.current || !cached.current) return;
      const audio = new Audio(cached.current.url);
      player.current = audio;
      audio.onended = () => {
        if (current === generation.current) setState("idle");
      };
      audio.onerror = () => {
        if (current === generation.current) {
          setState("idle");
          setError("Audio could not play. Tap Read aloud to try again.");
        }
      };
      await audio.play();
      if (current !== generation.current) {
        audio.pause();
        return;
      }
      setState("playing");
    } catch (e) {
      if (current !== generation.current) return;
      setState("idle");
      setError(
        e instanceof DOMException && e.name === "NotAllowedError"
          ? "Tap Read aloud on the reply to allow audio playback."
          : "Speech is unavailable. Your text reply is still available; try Read aloud again later.",
      );
    }
  };
  return { play, stop, state, error };
}
