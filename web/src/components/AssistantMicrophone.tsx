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
}: {
  value: string;
  onChange: (value: string) => void;
  active: boolean;
  disabled: boolean;
  onListening: (listening: boolean) => void;
}) {
  const recognition = useRef<Recognition | null>(null);
  const [listening, setListening] = useState(false);
  const [message, setMessage] = useState("");
  const changeRef = useRef(onChange);
  const listeningRef = useRef(onListening);
  changeRef.current = onChange;
  listeningRef.current = onListening;
  const finish = () => {
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
      const current = recognition.current;
      recognition.current = null;
      if (current) {
        current.onresult = current.onerror = current.onend = null;
        current.abort();
      }
      listeningRef.current(false);
    };
  }, [active]);

  const toggle = () => {
    if (listening) {
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
      return;
    }
    if (!Constructor) {
      setMessage(
        "Voice input is unavailable in this browser. Use your phone keyboard’s dictation button to speak into the message.",
      );
      return;
    }
    const current = new Constructor();
    const prefix = value.trimEnd();
    current.lang = navigator.language || "en-US";
    current.continuous = false;
    current.interimResults = true;
    current.onresult = (event) => {
      if (recognition.current !== current) return;
      const text = Array.from(
        event.results,
        (result) => result[0].transcript,
      ).join(" ");
      changeRef.current((prefix ? prefix + " " : "") + text);
    };
    current.onerror = (event) => {
      if (recognition.current !== current) return;
      setMessage(
        event.error === "not-allowed" || event.error === "service-not-allowed"
          ? "Microphone permission was denied. Allow microphone access in your browser settings, or use keyboard dictation."
          : event.error === "no-speech"
            ? "No speech detected. Tap the microphone to try again."
            : event.error === "aborted"
              ? ""
              : "Voice input could not connect. Try again or use keyboard dictation.",
      );
      finish();
    };
    current.onend = () => {
      if (recognition.current !== current) return;
      recognition.current = null;
      finish();
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
      setMessage(
        "Microphone could not start. Try again or use keyboard dictation.",
      );
    }
  };
  return (
    <div className="assistant-voice">
      <button
        type="button"
        aria-label={listening ? "Stop microphone" : "Use microphone"}
        aria-pressed={listening}
        disabled={disabled}
        onClick={toggle}
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
      <span role="status">
        {listening
          ? "Listening… Tap Stop, review your message, then Send."
          : message}
      </span>
    </div>
  );
}
