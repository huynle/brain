import { useEffect, useRef, useState } from "react";

type Recognition = {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onresult:
    | ((event: {
        results: ArrayLike<ArrayLike<{ transcript: string }>>;
      }) => void)
    | null;
  onaudiostart?: (() => void) | null;
  onerror: ((event: { error: string }) => void) | null;
  onend: (() => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
};
type SpeechWindow = Window & {
  SpeechRecognition?: new () => Recognition;
  webkitSpeechRecognition?: new () => Recognition;
};

/** Dictation stays in the draft for review; recording never sends a message. */
export function AssistantMicrophone({
  value,
  onChange,
  active,
  disabled,
  onListening,
  handsFree = false,
  onHandsFreeChange,
  onTurn,
}: {
  value: string;
  onChange: (value: string) => void;
  active: boolean;
  disabled: boolean;
  onListening: (listening: boolean) => void;
  handsFree?: boolean;
  onHandsFreeChange?: (enabled: boolean) => void;
  onTurn?: (text: string) => void;
}) {
  const recognition = useRef<Recognition | null>(null);
  const silence = useRef<ReturnType<typeof setTimeout>>();
  const emptyTurns = useRef(0);
  const [cycle, setCycle] = useState(0);
  const [receivingAudio, setReceivingAudio] = useState(false);
  const handsFreeRef = useRef(handsFree);
  const turnRef = useRef(onTurn);
  const modeRef = useRef(onHandsFreeChange);
  handsFreeRef.current = handsFree;
  turnRef.current = onTurn;
  modeRef.current = onHandsFreeChange;
  const pause = () => {
    handsFreeRef.current = false;
    modeRef.current?.(false);
    clearTimeout(silence.current);
    const current = recognition.current;
    recognition.current = null;
    current?.abort();
    finish();
  };
  const [listening, setListening] = useState(false);
  const [message, setMessage] = useState("");
  const changeRef = useRef(onChange);
  const listeningRef = useRef(onListening);
  changeRef.current = onChange;
  listeningRef.current = onListening;
  const finish = () => {
    setReceivingAudio(false);
    setListening(false);
    listeningRef.current(false);
  };
  useEffect(() => {
    if (!active) {
      recognition.current?.abort();
      recognition.current = null;
      finish();
    }
    return () => {
      clearTimeout(silence.current);
      handsFreeRef.current = false;
      modeRef.current?.(false);
      const current = recognition.current;
      recognition.current = null;
      if (current) {
        current.onresult = current.onerror = current.onend = null;
        current.abort();
      }
      listeningRef.current(false);
    };
  }, [active]);

  const toggle = (auto = false) => {
    if (recognition.current) {
      recognition.current?.stop();
      return;
    }
    const speechWindow = window as SpeechWindow;
    const Constructor =
      speechWindow.SpeechRecognition ?? speechWindow.webkitSpeechRecognition;
    if (!window.isSecureContext) {
      setMessage(
        "Microphone needs an HTTPS connection. You can also use your phone keyboard’s dictation button.",
      );
      pause();
      return;
    }
    if (!Constructor) {
      setMessage(
        "Voice input is unavailable in this browser. Use your phone keyboard’s dictation button to speak into the message.",
      );
      return;
    }
    if (!Constructor) return;
    if (auto) {
      handsFreeRef.current = true;
      modeRef.current?.(true);
    }
    let transcript = "";
    let failed = false;
    const current = new Constructor();
    const prefix = value.trimEnd();
    current.lang = navigator.language || "en-US";
    current.continuous = auto;
    current.interimResults = true;
    current.onaudiostart = () => {
      if (recognition.current === current) setReceivingAudio(true);
    };
    current.onresult = (event) => {
      if (recognition.current !== current) return;
      setReceivingAudio(true);
      const text = Array.from(
        event.results,
        (result) => result[0].transcript,
      ).join(" ");
      transcript = (prefix ? prefix + " " : "") + text;
      changeRef.current(transcript);
      clearTimeout(silence.current);
      if (auto && text.trim())
        silence.current = setTimeout(() => {
          if (recognition.current === current) current.stop();
        }, 1100);
    };
    current.onerror = (event) => {
      if (recognition.current !== current) return;
      if (auto && event.error === "no-speech") {
        // Silence is a normal hands-free state. onend starts a fresh recognizer.
        clearTimeout(silence.current);
        return;
      }
      failed = true;
      clearTimeout(silence.current);
      setMessage(
        event.error === "not-allowed" || event.error === "service-not-allowed"
          ? "Microphone permission was denied. Allow microphone access in your browser settings, or use keyboard dictation."
          : event.error === "no-speech"
            ? "No speech detected. Tap the microphone to try again."
            : event.error === "aborted"
              ? ""
              : "Voice input could not connect. Try again or use keyboard dictation.",
      );
      if (auto) {
        handsFreeRef.current = false;
        modeRef.current?.(false);
      }
      finish();
    };
    current.onend = () => {
      if (recognition.current !== current) return;
      recognition.current = null;
      clearTimeout(silence.current);
      finish();
      setCycle(n => n + 1);
      if (auto && handsFreeRef.current && !failed) {
        if (transcript.trim()) {
          emptyTurns.current = 0;
          turnRef.current?.(transcript.trim());
        } else {
          emptyTurns.current++;
        }
      }
    };
    recognition.current = current;
    setMessage("");
    setListening(true);
    listeningRef.current(true);
    try {
      current.start();
    } catch {
      recognition.current = null;
      finish();
      if (auto) pause();
      setMessage(
        "Microphone could not start. Try again or use keyboard dictation.",
      );
    }
  };
  useEffect(() => {
    if (!handsFree || !active || disabled || recognition.current) return;
    const timer = setTimeout(() => toggle(true), emptyTurns.current > 0 ? 1000 : 50);
    return () => clearTimeout(timer);
  }, [handsFree, active, disabled, listening, cycle]);
  useEffect(() => {
    const hidden = () => {
      if (document.hidden) pause();
    };
    document.addEventListener("visibilitychange", hidden);
    return () => document.removeEventListener("visibilitychange", hidden);
  }, []);
  return (
    <div className="assistant-voice">
      <button
        type="button"
        aria-label={listening ? "Stop microphone" : "Use microphone"}
        aria-pressed={listening}
        disabled={disabled || handsFree}
        onClick={() => toggle()}
      >
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          aria-hidden="true"
        >
          <rect x="9" y="2" width="6" height="12" rx="3" />
          <path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8" />
        </svg>{" "}
        {listening ? "Stop listening" : "Speak"}
      </button>
      {onHandsFreeChange && (
        <button
          type="button"
          aria-pressed={handsFree}
          disabled={!handsFree && (disabled || listening)}
          onClick={() => {
            if (handsFree) pause();
            else {
              emptyTurns.current = 0;
              toggle(true);
            }
          }}
        >
          {handsFree ? "End hands-free" : "Start hands-free"}
        </button>
      )}
      <span role="status">
        {listening
          ? handsFree
            ? receivingAudio ? "Listening… Your message sends after a pause." : "Starting speech recognition…"
            : "Listening… Tap Stop, review your message, then Send."
          : handsFree && disabled
            ? "Waiting for Assistant…"
            : message}
      </span>
    </div>
  );
}
