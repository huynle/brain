# SDK review regressions

Independent review `keai8keu` rejected candidate `c26819a8`. The following
regressions reproduce its four findings before their fixes:

- `TestBodylessMutationDoesNotReplayAfterLostResponse`: prime a real HTTP
  persistent connection, receive a keyed attachment DELETE, close the socket
  without a response, assert exactly one mutation. The original client sends two.
  A non-rewindable empty stream now prevents bodyless keyed mutations from being
  classified replayable by Go's default transport; idempotency headers remain.
- Node `ordinary Node inspection does not disclose response content`: real 403
  response, inspect and Console.error as well as JSON serialization. Wire fields
  remain explicitly readable but are non-enumerable; Node default inspection is
  sanitized. Explicit inspection with custom inspection disabled and hidden fields
  enabled is intentional structured access, not a safe logging interface.
- Node `mid-response disconnect preserves SDK error metadata`: real listener
  sends headers and partial body; destroy socket after native fetch receives
  headers. Native `TypeError: terminated` is replaced with `response_read_failed`
  retaining HTTP status and request ID. Aborted signals still retain their reason.
- Both clients reject structural dot segments centrally before transport,
  including repeatedly encoded dots and dots within slash/backslash-delimited
  identifiers. No request reaches the regression listener. This is routing
  defense, not an authorization or confinement claim.

These fixes do not enable scripts, supply hosted authorization, or complete V1.
The newer reminder/attention clients remain single-profile wrappers over existing
server semantics, not evidence of future tenant ACL/publication composition.
