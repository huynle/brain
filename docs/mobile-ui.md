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

Hands-free mode automatically sends a recognized turn after a 1.1-second
pause, waits for the reply and audio to finish, then listens again. It requires
HTTPS and browser speech recognition. End hands-free, permission errors, hiding
the page, or closing Assistant stop the loop. During playback, a local echo-cancelled microphone level detector watches for
180 ms of sustained sound and stops audio, then rearms speech recognition.
It does not buffer the interrupting audio, so the opening syllable may be missed.
Actual echo rejection depends on the phone/browser; an Interrupt and speak button
provides a fallback. Voice requests prefer one or two short sentences unless
more detail is requested.
Saved conversations remain local to this browser; server conversation storage is not implemented. Speech provider and UI checks can be run with
`go test ./internal/api` and `node web/scripts/verify-assistant-speech.mjs`.


Interruption capture is reopened for each reply, after browser recognition ends,
and starts while speech audio is loading. The interruption audio context exists only during a reply, so it does not compete with speech recognition for audio focus. The UI reports connecting, listening, or unavailable
capture (muted/ended track, suspended context, permission failure, or sustained
zero samples). Detection uses echo-cancelled microphone volume, not semantic VAD;
car Bluetooth routing and false triggers still require physical-device testing.
The Interrupt and speak button remains available as a manual fallback.

Speech recognition shows Starting until an audio-start or result event arrives. Hands-free stays enabled through long silence: no-speech and empty recognition endings restart the browser recognizer with a one-second delay. There is no forced no-transcript timeout. Explicit stop, permission errors, and leaving the page still stop hands-free.
