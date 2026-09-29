# Mobile dashboard

The dashboard uses the mobile layout at widths up to 900px, and on short
landscape touch screens. The same workspace stores, task actions, editors,
and API routes serve both layouts.

- Open the menu beside **brain** for project search, hidden projects, runners,
  saved views, and settings. **More** contains theme and side panel controls.
- Overview, Focus, Entries, and live sessions remain in the scrolling navigation.
- Task details and the assistant fill the screen and have explicit close buttons.
- Focus tabs expose **Pane actions** for closing or splitting without a mouse.
  Splits stack vertically; their saved tree and desktop ratios are preserved.
- Session metadata is available under **Session details**.
- Sync status has reserved space below the workspace. Background operations and
  reminders are available from **Activity**, without covering the workspace.
- Dialogs follow the visual viewport when a keyboard changes its size. Pinch
  zoom remains enabled. Attachment previews remain in the current tab.

If persistent browser storage cannot be opened, the dashboard explains that it
is using online mode. Definition edits save directly to the server with the
editor's expected revision. Existing local data is not deleted or reset; pending
local edits cannot be inspected until storage becomes available again. Reload
to retry storage initialization. Offline editing still requires working storage.

## Local verification

Build with `just web-build && just build`, start an isolated development server,
then run from `web/`:

```sh
BRAIN_MOBILE_TEST_URL=http://localhost:3336 npm run test:mobile
BRAIN_MOBILE_TEST_URL=http://localhost:3336 BRAIN_MOBILE_BROWSER=webkit npm run test:mobile
npm run test:offline
BRAIN_READER_TEST_URL=http://localhost:3336 npm run test:reader
BRAIN_READER_TEST_URL=http://localhost:3336 npm run test:attachment-preview
BRAIN_READER_TEST_URL=http://localhost:3336 npm run test:cached-startup
```

The mobile script also checks sign-in field sizing and reduced-viewport scrolling.
It requires loopback and seeds a unique project with notes, a
task, a disabled automation, and an image attachment. It verifies 320px/390px
phones, tablet and landscape layouts, navigation/settings/search/assistant,
in-page previews across background updates, definition editing in a reduced
viewport, persisted task status changes, automation/goal controls, touch pane
splitting, entry comparison/diff, returning to desktop, and full-width session
transcripts/metadata using a controlled history response. Screenshots and a
results file are written to a unique temporary directory. Fixtures are retained
for manual review; no automation or agent execution is started.

The browser checks use Chromium and Playwright WebKit emulation, not physical
iOS/Android devices. A reduced viewport tests layout under constrained height;
it does not reproduce every native keyboard behavior. Live executor streaming
and all provider-specific runner operations are not exercised by this script.

## Thumb access and chat

Mobile starts at Overview, with explicit entry links preserved. The prior Assistant-home
preference is retired. A circular Assistant button is centered above the bottom sync
status bar. Assistant uses a scrollable bubble transcript and a fixed draft composer;
reading earlier messages does not automatically jump to the latest reply. Session details,
suggestions, and quick actions remain available in the expandable details area.

Speak uses browser speech recognition to fill the draft; the user reviews and sends it.
Browser support and microphone permission are required. HTTPS is required away from
localhost. Unsupported browsers and HTTP previews can use phone-keyboard dictation.
Recognition may use the browser vendor's online speech service. Closing chat stops it.
Conversations are saved on this device. Use the Conversation selector to reopen one, or New chat to start a separate history. Switching stops recording, playback and the active response; late callbacks cannot write into the selected conversation. This is not server-backed storage or cross-device synchronization. Each conversation retains up to 100 displayed turns and 200 replay messages.

### Optional spoken replies

The server accepts authenticated, admin-scoped `POST /api/v1/assistant/speech`
with `{ "text": "A reply to read" }` and returns MP3. Input is bounded to 6,000
characters and responses to 8 MiB. Provider credentials stay server-side.
Configure `server.assistant.speech` with `enabled: true`, `provider: openrouter`,
`model: hexgrad/kokoro-82m`, and `voice: af_heart`. Optional `base_url` and
`api_key_env` default to OpenRouter and `OPENROUTER_API_KEY`.

Read aloud plays individual replies. Spoken replies opts into playback of new
responses for the current visit. Stop, new chat, sending a message, starting the
microphone, and closing Assistant cancel playback. Replay of the last audio
uses an in-memory cache. Browser autoplay restrictions show a manual-play hint.
Audio failures leave the text conversation intact. Breeze is a future adapter
behind `SpeechSynthesizer`; only OpenRouter is implemented at present.

Hands-free now owns one getUserMedia stream and AudioWorklet for the entire
open session. Hands-free requests a screen wake lock automatically and shows
“Screen kept awake” when granted. Ending hands-free, switching chats, hiding the
page, or a startup/voice failure releases it. If unsupported, denied, or released
by the system, a non-blocking message explains that the screen may sleep. This
prevents automatic screen sleep only; it does not support a manually locked
phone.

A browser-local Silero V5 classifier keeps two seconds of pre-roll and submits a WAV
segment after 1.2 seconds of silence (30-second maximum per segment). Silence
alone never creates a transcription request. The stream remains open during
transcription, reasoning and playback. Sustained speech stops playback and
cancels the current reasoning request; the recorded new turn is retained.
Echo cancellation must be reported enabled for recording during playback;
otherwise use the manual Interrupt and speak button. Normal listening requires
220 ms above 0.65 speech probability; interruption requires 450 ms above 0.9.
Active speech continues above 0.4, allowing natural quiet syllables. These are
acoustic speech probabilities, not semantic topic or end-of-thought detection.
Car noise and Bluetooth routing still need device testing.

The pinned Silero model and ONNX Runtime WASM assets load only when hands-free
starts, from the same server. Versioned assets are cached on demand, not included
in the dashboard precache. No audio leaves the browser for speech detection.

Admin-scoped `POST /api/v1/assistant/transcribe` accepts base64 WAV in `audio`
and returns `text`. It uses the configured OpenRouter speech key/base URL and
`BRAIN_ASSISTANT_TRANSCRIPTION_MODEL` (default `openai/whisper-large-v3-turbo`).
Detected speech audio goes to OpenRouter and incurs transcription usage;
raw audio is not persisted or logged by Brain. There is no silence timeout.
Stop, closing the panel, hiding the page, or switching conversations releases
capture and cancels pending transcription. Queues are bounded; failures stop
hands-free visibly instead of silently dropping turns. Speak remains browser
one-shot dictation. Saved conversations remain local to the browser.

Verification: `node web/scripts/verify-screen-wake-lock.mjs` checks wake-lock
acquisition, release, denial, unsupported browsers and cancellation races.
`go test ./internal/api`, frontend `src/lib/voiceCapture.test.ts`,
`node web/scripts/verify-persistent-voice.mjs` (real classifier with speech/rumble fixtures and lifecycle/cancellation), and
`node web/scripts/verify-persistent-audio.mjs` (real Chromium capture/worklet with
a WAV microphone fixture). A live OpenRouter transcription was also verified.
Physical Android/car-Bluetooth stability is a separate user retest.

Voice diagnostics are posted through the authenticated admin-only
`POST /api/v1/assistant/voice-diagnostics` endpoint. Search container logs for
`assistant voice diagnostic`. Events share a random recognition-attempt ID and
include elapsed time, event type, standard error code, result-event count, Android
flag, and hands-free flag. No transcript, audio, full user-agent, or device name
is sent. Waiting is reported every 30 seconds without ending the session. The
server rejects unknown fields, unknown event/error codes and oversized requests.
Browser events are evidence about recognition progress, not proof that physical
microphone samples contain speech; the actual device still needs a test attempt.
