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
The current conversation is stored on this device; this is not a server-backed session list.
