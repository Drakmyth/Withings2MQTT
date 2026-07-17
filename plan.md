# Withings-to-MQTT: Incremental Delivery Plan

## Delivery Workflow

- Create one short-lived feature branch per pull request.
- For every implementation step: implement one feature, add or adjust its automated tests, run the relevant test suite, commit the change, and push it before beginning the next step.
- Open PRs only after a complete vertical milestone. Do not merge a PR until the user has reviewed and explicitly approved it.
- Keep commits atomic and descriptive. PR descriptions list included steps, tests run, configuration changes, and migration impact.

## Pull Requests and Implementation Steps

### Frontend Delivery Requirements

- Treat each pull request as a complete operator workflow: data added by a milestone must be discoverable, inspectable, and actionable from `/admin` in that same milestone.
- Use server-rendered `html/template` pages with semantic, accessible HTML and locally served CSS. HTMX may enhance refreshes, filters, pagination, and form feedback, but every workflow must remain functional with ordinary links, forms, and redirects.
- Keep large histories bounded with server-side pagination and filtering. Never render an account's complete measurement history in one response.
- Show useful empty, loading, success, and failure states. Display timestamps, units, last-success information, and actionable error summaries where relevant.
- Keep the frontend responsive for phone and desktop use without adding a frontend build system, JavaScript framework, external CDN, or HTMX extension.

### PR 1 — Server Foundation

1. Rename the binary and remove Cobra; verify `go build`.
2. Add typed environment configuration; verify invalid configuration fails clearly.
3. Start a `net/http` server; verify it accepts requests.
4. Add `GET /healthz`; verify HTTP 200.
5. Add graceful shutdown; verify in-flight request behavior.
6. Update Docker build/run; verify container health check.
7. Open SQLite using `modernc.org/sqlite`; verify configured DB opens.
8. Add embedded, ordered SQL migrations; verify idempotent restart.
9. Add linked-user persistence; verify restart retention.
10. Add AES-GCM OAuth-token encryption; verify no plaintext is stored.
11. Serve `/admin` using `html/template`; verify initial status display.
12. Vendor and locally serve a pinned HTMX asset; verify no external asset request.
13. Bind admin to localhost by default; verify remote access is rejected.

### PR 2 — Withings Account Linking

14. Validate Withings OAuth configuration; verify incomplete settings reject linking.
15. Start authorization redirect; verify required URL parameters.
16. Create and validate one-time OAuth state; verify invalid, reused, and expired state fails.
17. Complete OAuth callback; verify mocked callback creates linked account.
18. Display linked accounts and status; verify persistence after restart.
19. Add HTMX account-status refresh every 30 seconds; verify full-page fallback works.
20. Unlink an account; verify removal prevents later synchronization.
21. Refresh expired access tokens; verify replacement tokens persist.

### PR 3 — Scale Data

22. Fetch Withings weight measurements; verify mocked decoding.
23. Persist normalized weight samples; verify restart retention.
24. Deduplicate imports; verify repeated data creates no duplicates.
25. Track measurement cursors; verify failures do not advance cursors.
26. Add manual weight sync; verify import counts and status.
27. Add HTMX manual-sync status replacement; verify POST/redirect fallback remains functional.
28. Normalize body composition; verify missing optional data is harmless.
29. Add a responsive admin layout and locally served stylesheet; verify navigation and core actions work at phone and desktop widths.
30. Display each account's latest weight, available body-composition values, and last successful scale sync; verify empty and populated states.
31. Add paginated scale history with metric filtering; verify bounded queries, stable ordering, timestamps, values, and units.
32. Add HTMX history filtering and pagination; verify ordinary link/query-parameter fallbacks return complete pages.

### PR 4 — Activity and Sleep

33. Fetch daily activity; verify mocked decoding.
34. Persist steps; verify timestamp and units.
35. Persist distance, calories, and elevation when supplied; verify optional-value handling.
36. Add manual activity sync; verify activity cursor handling.
37. Fetch sleep summaries; verify mocked decoding.
38. Persist sleep sessions and summary metrics; verify duplicate prevention.
39. Add manual sleep sync; verify status reporting.
40. Add combined manual sync; verify category failures are isolated.
41. Display latest activity and sleep summaries per account; verify empty, partial, and populated states.
42. Add paginated activity history and sleep-session views; verify bounded queries and stable ordering.
43. Add category-aware manual-sync controls and feedback; verify individual and combined sync workflows without JavaScript.
44. Add HTMX activity and sleep view refreshes; verify full-page filtering and pagination fallbacks.

### PR 5 — Background Sync

45. Add periodic synchronization with configurable interval; verify it runs without admin interaction.
46. Isolate account/category failures with bounded retry; verify one failure does not halt others.
47. Display scheduler configuration, current state, last run, next run, and per-account/category outcomes; verify restart and failure states.
48. Add HTMX background-status refresh; verify the full admin page reports the same state without JavaScript.

### PR 6 — MQTT Publishing

49. Validate MQTT configuration; verify invalid settings fail clearly.
50. Connect with Eclipse Paho; verify test-broker connection and admin status.
51. Publish retained weight JSON; verify expected topic and payload.
52. Publish body-composition metrics; verify matching messages.
53. Publish activity metrics; verify steps topic and payload.
54. Publish sleep metrics; verify expected metric messages.
55. Record MQTT delivery only after acknowledgement; verify outage leaves samples unpublished.
56. Republish outstanding samples after reconnect; verify outage-period data catches up.
57. Display broker connection state, last error, last successful publish, and outstanding delivery count; verify connected, disconnected, and backlog states.
58. Add HTMX MQTT-status refresh; verify the same diagnostics render in the full-page fallback.

### PR 7 — Webhooks and Documentation

59. Enable webhook route only with public callback configuration; verify disabled default.
60. Accept Withings notifications and promptly return 2xx; verify no fetch blocks response.
61. Queue cursor-based sync from a notification; verify data imports and publishes.
62. Display webhook enablement, callback readiness, last notification, and queued-sync outcome; verify disabled and active states.
63. Add an admin overview that summarizes account, sync, data, MQTT, and webhook health with links to detailed views; verify every operational state is reachable without JavaScript.
64. Perform an accessibility and responsive-layout pass across all admin workflows; verify keyboard navigation, labels, landmarks, focus visibility, and narrow-screen layouts.
65. Document configuration, secrets, defaults, and local data paths; verify clean setup.
66. Document Withings developer app, OAuth, scopes, and linking; verify operator walkthrough.
67. Document MQTT contract, Docker, SQLite backup, webhook proxying, and local HTMX asset; verify consumer/deployment walkthrough.

## Dependencies

- Go standard library for HTTP, templates, configuration, OAuth requests, encryption, scheduling, logging, and tests.
- `modernc.org/sqlite` for SQLite.
- `github.com/eclipse/paho.mqtt.golang` for MQTT.
- A pinned, vendored HTMX browser asset for progressive enhancement only.

Do not add a web framework, ORM, migration framework, OAuth/configuration library, assertion framework, frontend build system, or HTMX extensions.

## Public Interfaces

- `GET /healthz`
- Local admin UI: `/admin`
- Internal HTMX fragments under `/admin`, with standard form-page fallbacks
- OAuth callback: `/oauth/withings/callback`
- Optional webhook: `/webhooks/withings`
- MQTT topic: `withings/<withings-user-id>/<metric>`
- Retained MQTT payload:

  ```json
  {"value":1234,"unit":"steps","observed_at":"2026-07-16T00:00:00Z","source":"withings"}
  ```

## Assumptions

- Withings OAuth developer credentials are available.
- v1 supports activity, sleep summaries, weight, and body composition.
- The installation serves a trusted single household and can link multiple Withings accounts.
- Admin access is localhost-only by default; public HTTPS is required only for optional webhooks.
- HTMX progressively enhances status refresh, filtering, pagination, and form feedback; all administrative flows work without JavaScript.
