# Changelog

This file summarizes the user-facing changes in every published release. Dates are the GitHub publication dates. See each linked release for its downloads and checksums.

## [1.9.8] - 2026-11-01 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.8))

- Fixed browser proof capture hanging for 30 seconds when the chat service shows its anonymous usage limit ("Daily limit reached"). The limit banner and wording are now detected and reported as a rate limit.
- Prefer an installed Chrome/Chromium build older than 156 when available, avoiding the new anonymous daily usage limit (version-aware browser selection, fallback to newest).
- Voice chats (/speak) now carry the current conversation context and record transcripts back into the session, with a grace period for flushing on close.
- Updated protocol notes and regression coverage.

## [1.9.7] - 2026-11-01 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.7))

- Added conversation branching, response retry, per-session notes, context pinning and compaction, and source comparison, auditing, and saved source sets.
- Large pasted text stays out of the visible prompt: the terminal shows a compact paste placeholder while the full text remains available to the submitted message.
- Refined the compact voice window and its size, voice, microphone, subtitle, and playback settings; improved popup sizing, including on Hyprland.
- Added dashboard access to the system tray, with `Ctrl+Alt+D` for text chat and `Ctrl+Alt+V` for voice chat by default.
- Added configurable response-frame width and borders, and improved dashboard window controls and sidebar toggling.

## [1.9.6] - 2026-10-28 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.6))

- Refreshed Go dependencies and improved Duck.ai browser capture and voice startup reliability.
- Expanded release verification for Linux, Windows, and macOS builds; fixed Windows ARM64 executable checks to recognize the AArch64 label.

## [1.9.5] - 2026-10-15 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.5))

- Added `/coordinate` to run two or three models in chain, independent, or debate workflows, resume incomplete stages, and optionally save reports.
- Added saved-session management, richer context and source inspection, source provenance, bounded PDF context, and one-shot prompt context flags.
- Added `duckchat doctor`, a cancellable streaming chat API endpoint, secure dashboard session actions, and guarded configuration reset.
- Fixed streaming completion and update-version reporting.

## [1.9.4] - 2026-10-11 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.4))

- Improved image and saved-session workflows.
- Fixed Darwin cross-compilation in the release build.

## [1.9.3] - 2026-10-11 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.3))

- Fixed normalization of Duck.ai image attachments.

## [1.9.2] - 2026-10-11 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.2))

- Restored the Duck.ai image-edit request flow.
- Refreshed CLI help, screenshots, and project documentation.

## [1.9.1] - 2026-10-10 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.1))

- Refined CLI presentation and interaction ahead of the 1.9 release series.

## [1.9.0] - 2026-10-10 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.9.0))

- Improved chat retry behavior, response rendering, and desktop interaction.
- Made terminal resize handling portable across supported systems.

## [1.8.1] - 2026-10-09 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.8.1))

- Added the MIT license and refreshed the README, command help guidance, and linked screenshots.
- Documentation-only maintenance release; no CLI behavior changes.

## [1.8.0] - 2026-10-08 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.8.0))

- Added voice chat and image workflows and rendered interactive help beside the CLI logo.
- Fixed CI import formatting and guarded security-report uploads.

## [1.7.0] - 2026-10-06 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.7.0))

- Added image attachments and dashboard analytics.
- Aligned release documentation and project files.

## [1.6.0] - 2026-10-06 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.6.0))

- Added the local usage dashboard, lifecycle commands, recent activity, model statistics, and bounded AI analysis.
- Added persistent conversation and analytics history, dashboard settings, and a loopback-only server.

## [1.5.2] - 2026-09-20 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.5.2))

- Added a browser-authentication spinner and Ctrl+C cancellation that keeps the CLI open.
- Improved model, search, file, scraping, API, and update flows; updates can be checked in the background.
- Preserved conversation history after failed or cancelled requests and added limits and timeouts for network work and context size.
- Removed the PMP integration and its `/pmp` command.

## [1.5.1] - 2026-09-20 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.5.1))

- Fixed normalization of Duck.ai model IDs.

## [1.5.0] - 2026-09-20 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.5.0))

- Hardened and refactored CLI and API request flows.

## [1.4.0] - 2026-09-20 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.4.0))

- Added opt-in Duck.ai native web search with citations and native image generation.
- Added support for decoding generated images from streaming responses and saving them in the export directory.
- Added the **Duck.ai Native Tools** configuration section.

## [1.3.0] - 2026-09-20 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.3.0))

- Migrated from the retired DuckDuckGo Chat protocol to the current Duck.ai protocol.
- Added browser-based proof-header capture, durable-stream RSA payloads, current model IDs, and aliases for older model settings.
- Updated protocol documentation and added protocol and model regression tests.

## [1.2.2] - 2025-07-11 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.2.2))

- Added `/load` to restore saved conversations.
- Cleaned up the browser VQD flow and removed terminal media notifications.

## [1.2.1] - 2025-07-09 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.2.1))

- Added a notification action for opening the project's issue-reporting page.

## [1.2.0] - 2025-07-08 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.2.0))

- Added `/update`, generated API documentation, and command chaining for adding context before a prompt.
- Fixed `/exit` handling and release-tag creation.

## [1.1.10] - 2025-07-08 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.10))

- Historical duplicate release: this tag points to the same commit as v1.2.0 and contains no separate code changes.

## [1.1.9] - 2025-06-29 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.9))

- Improved streamed response rendering and added protection for long terminal input.
- Fixed handling of canceled or invalid input-protection settings.

## [1.1.8] - 2025-06-29 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.8))

- Improved Markdown rendering and fixed response re-rendering.

## [1.1.7] - 2025-06-27 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.7))

- Added `/library` and `/pmp` commands.
- Improved `/search`, `/file`, and `/url`, including asking a question about supplied context.

## [1.1.6] - 2025-06-26 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.6))

- Added automated CI builds and macOS support in the build workflow.
- Stabilized CI and security scanning and fixed Duck.ai VQD handling.

## [1.1.5] - 2025-06-14 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.5))

- Added `/version` with CLI, Go, and system information.
- Added dynamic request-header extraction and build-time version injection; improved error handling.
- Corrected model naming in configuration and documentation.

## [1.1.4] - 2025-03-29 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.4))

- Fixed a Duck.ai HTTP 418 response issue.
- Added global context settings in `/config`.

## [1.1.3] - 2025-02-27 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.3))

- Restored Chromium support and updated the Mixtral Small 3 model entry.
- Refreshed project documentation.

## [1.1.2] - 2025-02-16 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.2))

- Split the application into packages and added persistent configuration through `/config`.
- Added the `o3-mini` model and improved model switching.
- Added `/copy`, richer `/export` formats, and system clipboard support.
- Improved retries, error handling, syntax highlighting, and terminal response display.

## [1.1.1] - 2025-02-13 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.1))

- Updated the Llama model entry and added `o3-mini`.

## [1.1.0] - 2025-02-02 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.1.0))

- Introduced the interactive CLI and commands for search, local files, URLs, history, model selection, context clearing, Markdown export, and message extraction.
- Added streamed responses and support for multiple Duck.ai models.

## [1.0.0] - 2024-09-26 ([release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/tag/v1.0.0))

- First published release: interactive terminal chat, multiple models, streamed responses, colored output, and DuckDuckGo terms acceptance.
