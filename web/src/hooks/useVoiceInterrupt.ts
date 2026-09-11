import { useEffect, useRef, useState } from "react";

/** Require sustained sound so brief clicks don't interrupt playback. */
export class VoiceInterruptDetector {
  private since: number | null = null;
  reset() {
    this.since = null;
  }
  update(level: number, now: number): boolean {
    if (level < 0.035) {
      this.reset();
      return false;
    }
    this.since ??= now;
    return now - this.since >= 180;
  }
}

// Keep the echo-cancelled input warm while hands-free is enabled. Recognition
// starts after interruption; this listener never sends audio to a provider.
export function useVoiceInterrupt(
  enabled: boolean,
  speaking: boolean,
  interrupt: () => void,
) {
  const speakingRef = useRef(speaking);
  const callback = useRef(interrupt);
  const [unavailable, setUnavailable] = useState(false);
  speakingRef.current = speaking;
  callback.current = interrupt;
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    let stream: MediaStream | undefined;
    let context: AudioContext | undefined;
    let timer: ReturnType<typeof setInterval> | undefined;
    const detector = new VoiceInterruptDetector();
    const cleanup = () => {
      clearInterval(timer);
      stream?.getTracks().forEach((track) => track.stop());
      if (context && context.state !== "closed") void context.close();
    };
    setUnavailable(false);
    void (async () => {
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          audio: {
            echoCancellation: true,
            noiseSuppression: true,
            autoGainControl: false,
          },
        });
        if (cancelled) {
          cleanup();
          return;
        }
        if (
          stream.getAudioTracks()[0]?.getSettings().echoCancellation === false
        ) {
          cleanup();
          setUnavailable(true);
          return;
        }
        context = new AudioContext();
        await context.resume();
        if (cancelled) {
          cleanup();
          return;
        }
        const source = context.createMediaStreamSource(stream);
        const analyser = context.createAnalyser();
        analyser.fftSize = 1024;
        source.connect(analyser);
        const samples = new Float32Array(analyser.fftSize);
        timer = setInterval(() => {
          if (!speakingRef.current || document.hidden) {
            detector.reset();
            return;
          }
          analyser.getFloatTimeDomainData(samples);
          let sum = 0;
          for (const value of samples) sum += value * value;
          if (
            detector.update(Math.sqrt(sum / samples.length), performance.now())
          ) {
            speakingRef.current = false;
            detector.reset();
            callback.current();
          }
        }, 30);
      } catch {
        cleanup();
        if (!cancelled) setUnavailable(true);
      }
    })();
    return () => {
      cancelled = true;
      cleanup();
    };
  }, [enabled]);
  return unavailable;
}
