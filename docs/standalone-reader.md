# Standalone Markdown reader

Use `/read.html?entry=<encoded-path-or-short-id>` on any deployed Brain origin:

- `http://localhost:3333/read.html?entry=abcdefgh`
- `https://brain.huynle.com/read.html?entry=projects%2Fexample%2Fscratch%2Fabcdefgh.md`

Use the dashboard entry's **Read only ↗** link to open a shareable reader URL.
The destination must have this build deployed and contain the referenced entry.
A localhost URL refers to the device opening it; use the hosted URL when sharing
across devices. Reader links grant no access: private entries still require the
existing sign-in flow. The OAuth callback returns to the reader URL and heading.

The page is a separate Vite entry point with responsive, printable Markdown,
authenticated attachment images, on-demand Mermaid, wiki links with aliases,
relative Markdown links, short IDs, same-origin entry URLs, heading anchors,
and forward-link/backlink navigation. Browser Back/Forward and modified clicks
use normal anchors. External links remain external, and raw HTML is not executed.
Graph links use Brain's existing index, so out-of-band files still need indexing.
Wiki references must identify an entry by path or short ID, not arbitrary titles.

A fresh visit requests only authentication status, the requested document and
its graph connections (plus any embedded assets). It neither starts the dashboard
nor bootstraps SQLite/WASM, polls tasks, or registers a service worker. An already
installed, updated PWA excludes reader URLs, including their query strings,
from its dashboard navigation fallback. Existing installations must accept the
new build before their old service worker can learn this route. Reader content
uses the server directly; it does not display unsynced browser drafts or promise
offline reading. Diagrams load their engine only when a diagram is present.

Verification: build with `just build-all`, start the resulting local server, and
run `npm --prefix web run test:reader`. This seeds a unique demo project on
loopback port 3333 (override with `BRAIN_READER_TEST_URL`) and exercises cold
loading, links, history, anchors, error states, mobile layout, auth gating, and
an installed service worker. It prints a reader URL and an evidence directory.

Verified locally: seven browser scenarios, 1,173 web tests, the 13-scenario
offline sync regression suite, the embedded web handler tests, and the combined
web/Go build passed. The auth gate test simulates an unauthorized response; a
full hosted OAuth login and production deployment have not been exercised.
