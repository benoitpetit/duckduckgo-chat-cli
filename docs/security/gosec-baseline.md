# Gosec baseline

The reviewed baseline was captured on 2026-10-31 with the CI-pinned gosec v2.30.0 command and Go 1.27.3. The machine-readable SARIF report is [gosec-baseline.sarif](gosec-baseline.sarif). It contains 42 findings.

| Rule | SARIF level | Findings | Triage |
| --- | --- | ---: | --- |
| G103 | warning | 3 | Unsafe calls remain limited to the Linux PTY test helper's ioctl boundary. |
| G115 | error | 10 | `image.Color.RGBA` values are bounded to 16 bits by its contract; the unpremultiplication path also clamps its result. |
| G124 | error | 6 | The dashboard session is HttpOnly and SameSite Strict on loopback HTTP; Duck.ai browser cookies preserve their Secure flag; the analysis jar uses fixed Duck.ai cookies with Secure enabled. |
| G204 | error | 14 | Subprocess calls use fixed commands, allowlisted terminal/browser executables, or argv without a shell; the Windows title command only receives a built-in model name. |
| G602 | warning | 3 | `xtermColor` receives values from a bounded 0–255 loop, and each branch constrains its indexes. |
| G703 | error | 2 | Desktop integration writes use a path under the user's config/data directory and constant application IDs; request or chat input does not form the path. |
| G704 | error | 2 | Production constructs the client with the fixed `https://duck.ai` base URL and fixed endpoint paths; redirects are refused to prevent proof-header forwarding. |
| G706 | warning | 2 | Debug-only origin/path logs format untrusted values with `%q`, which escapes newlines. |

The reviewed G104, G301, G302, G304, and G306 findings are resolved. New config, transcript, analytics, image, update-cache, and optional debug-log files use owner-only permissions. Desktop integration retains the standard discoverable directory/file modes, and verified update binaries retain executable mode; these call sites have narrow, rule-specific explanations. Remaining G304 paths are either fixed per-user application paths, validated session/update paths, or explicit user-selected imports that are type- and size-checked. Hash writes rely on the `hash.Hash` contract, while meaningful read, write, close, and cleanup errors are returned or deliberately recorded as advisory failures.

The CI comparison checks each new SARIF result against this reviewed snapshot by rule, file, source snippet, and message. A result absent from the snapshot fails the check and remains visible in the uploaded report. Update the snapshot only after reviewing each finding and recording its disposition here; do not use a baseline update to silence an unexplained finding.
