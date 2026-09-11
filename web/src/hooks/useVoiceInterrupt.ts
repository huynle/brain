import { useEffect, useRef, useState } from "react";

/** Require sustained sound so brief clicks don't interrupt playback. */
export class VoiceInterruptDetector {
  private since: number | null = null;
  reset() { this.since = null; }
  update(level: number, now: number): boolean {
    if (level < 0.015) { this.reset(); return false; }
    this.since ??= now;
    return now - this.since >= 180;
  }
}

type InterruptStatus = "idle" | "starting" | "listening" | "unavailable";

// Recognition and this capture stream take turns owning the microphone. In
// particular, don't keep a possibly muted stream from a prior recognition turn.
export function useVoiceInterrupt(enabled: boolean, monitoring: boolean, speaking: boolean, interrupt: () => void) {
  const speakingRef = useRef(speaking);
  const callback = useRef(interrupt);
  const [status, setStatus] = useState<InterruptStatus>("idle");
  speakingRef.current = speaking;
  callback.current = interrupt;

  useEffect(() => {
    if (!enabled || !monitoring) { setStatus("idle"); return; }
    let cancelled = false;
    let stream: MediaStream | undefined;
    let context: AudioContext | undefined;
    let source: MediaStreamAudioSourceNode | undefined;
    let timer: ReturnType<typeof setInterval> | undefined;
    let watchdog: ReturnType<typeof setTimeout> | undefined;
    const detector = new VoiceInterruptDetector();
    const cleanup = () => {
      clearInterval(timer);
      clearTimeout(watchdog);
      source?.disconnect();
      if (context && context.state !== "closed") void context.close().catch(() => {});
      stream?.getTracks().forEach(track => track.stop());
    };
    setStatus("starting");
    // A pending permission/resume promise must not masquerade as readiness.
    watchdog = setTimeout(() => { if (!cancelled) setStatus("unavailable"); }, 3000);
    void (async () => {
      try {
        stream = await navigator.mediaDevices.getUserMedia({audio: {
          echoCancellation: true, noiseSuppression: true, autoGainControl: true,
        }});
        if (cancelled) { cleanup(); return; }
        const track = stream.getAudioTracks()[0];
        if (!track || track.getSettings().echoCancellation === false) {
          cleanup(); setStatus("unavailable"); return;
        }
        context = new AudioContext();
        const audioContext = context;
        await audioContext.resume();
        if (cancelled) { cleanup(); return; }
        source = audioContext.createMediaStreamSource(stream);
        const analyser = audioContext.createAnalyser();
        analyser.fftSize = 1024;
        source.connect(analyser);
        const samples = new Float32Array(analyser.fftSize);
        let resuming = false;
        let lastSignal = performance.now();
        clearTimeout(watchdog);
        timer = setInterval(() => {
          if (document.hidden) { detector.reset(); return; }
          if (audioContext.state !== "running" || track.muted || track.readyState === "ended") {
            setStatus("unavailable"); detector.reset();
            if (audioContext.state !== "running" && audioContext.state !== "closed" && !resuming) {
              resuming = true;
              void audioContext.resume().catch(() => {}).finally(() => { resuming = false; });
            }
            return;
          }
          analyser.getFloatTimeDomainData(samples);
          let sum = 0;
          for (const value of samples) sum += value * value;
          const level = Math.sqrt(sum / samples.length);
          const now = performance.now();
          if (level > 0.00001) lastSignal = now;
          // Some devices supply zeroes without emitting a track mute event.
          setStatus(now - lastSignal > 2500 ? "unavailable" : "listening");
          if (!speakingRef.current) { detector.reset(); return; }
          if (detector.update(level, now)) {
            speakingRef.current = false; detector.reset(); callback.current();
          }
        }, 30);
      } catch {
        cleanup();
        if (!cancelled) setStatus("unavailable");
      }
    })();
    return () => { cancelled = true; cleanup(); };
  }, [enabled, monitoring]);
  return { status };
}
