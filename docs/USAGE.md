# User guide

This guide covers the interactive CLI, its commands, and its optional tools. For installation and source builds, see the [README](../README.md) and [build guide](BUILD.md).

## Start a chat

Run `duckchat` without options to open the interactive chat. On first use, accept Duck.ai's terms. Enter a message at the `You:` prompt, or type `/help` to see the available commands.

Chrome or Chromium is required for Duck.ai chat and voice. Version 115 or newer is recommended. The CLI starts a separate headless browser profile for chat requests. It does not use your regular browser profile or a Chrome extension.

### One-shot prompts

Use `--prompt` to send one message and exit. Add `--model` to choose a model, and `--json` to write the result as JSON. Pass `-` as the prompt value to read from standard input. Add `--file`, `--url`, or `--search` one or more times to prepare context for the prompt.

```sh
duckchat --help
duckchat --version
duckchat --prompt "Explain the Go memory model"
duckchat --prompt "Explain the Go memory model" --model gpt-6-luna
duckchat --prompt "Summarize this input" --json
printf 'Summarize this input' | duckchat --prompt -
duckchat --prompt "Compare these sources" --file report.pdf --file chart.png --url https://example.com --search "latest release notes"
```

Context flags require `--prompt`; they also work with `--prompt -`. Sources are loaded in a stable order regardless of how the options are interleaved: all files first, then URLs, then searches. `--file` accepts supported text files, PDFs, and PNG/JPEG/WebP images. The combined text context limit is 2 MiB, and each image keeps its existing 10 MiB file limit. Invalid options are rejected before chat browser setup.

The first interactive run must accept the terms before one-shot prompts can run without a terminal. One-shot mode does not start the tray service, dashboard, API, or startup update check. Its response goes to standard output. Status and error messages go to standard error. JSON mode writes a single response or error object to standard output.

### Diagnostics

Run `duckchat doctor` for local checks of configuration, terms acceptance, Chrome or Chromium, and the export directory. This does not open an interactive chat. Network checks are disabled by default.

`duckchat doctor --network` performs a bounded Duck.ai connectivity and browser-capture check. It does not submit a prompt or create a conversation. `duckchat doctor --json` writes stable check IDs, statuses, and an exit code as JSON. Sensitive captured request data is omitted.

`duckchat doctor --help` shows all options.

```sh
duckchat doctor
duckchat doctor --network
duckchat doctor --json
```

## Chat commands

Type `/help` in the CLI for the current command list.

| Command | Description |
| --- | --- |
| `/clear` | Save the current conversation and start a fresh chat session |
| `/config` | Change the model, appearance, shortcuts, and other settings |
| `/coordinate [cancel|runs|show <run_id>]` | Coordinate models and browse saved reports |
| `/context [pin|unpin|pins|compact]` | Inspect context, pin items, or compact the transcript |
| `/sources [compare|audit|save|load|list|delete]` | Compare, audit, and reuse source references |
| `/copy` | Copy a response or code block |
| `/dashboard <on\|off\|open\|status>` | Control or open the local dashboard |
| `/exit` | Close the interactive CLI |
| `/export` | Save conversation content to a file |
| `/file <path> [-- prompt]` | Add a file to chat context or ask about it directly |
| `/history` | Search and browse saved conversations |
| `/sessions [query]` | Find, organize, restore, or delete saved sessions |
| `/branch [name]` | Create a branch from the active conversation |
| `/retry [instruction]` | Regenerate and replace the last chat answer |
| `/notes [show|add <text>|clear]` | Manage local notes for this session |
| `/image [idea]` | Generate an image, with an optional guided prompt flow |
| `/issue` | Fill in a report and open a prefilled GitHub issue |
| `/library [command] [args] [-- prompt]` | Manage local file libraries and use their content |
| `/load [session_id [--full]]` | Restore a saved conversation, optionally with its full transcript |
| `/model [model_name]` | Choose or change the model |
| `/prompt [command]` | Add, edit, list, load, or remove saved prompts |
| `/search <query> [-- prompt]` | Search the web and use results as chat context |
| `/speak` | Open or restore a voice conversation |
| `/stats` | Show current session statistics |
| `/update [--force]` | Check for or install an update |
| `/url <url> [-- prompt]` | Add a web page to chat context or ask about it directly |
| `/version` | Show version and system information |
| `/api [port]` | Start or stop the local HTTP API |

`/context` reports measured conversation text and image bytes separately, lists source-size metadata, and marks compressed, truncated, omitted, or pinned items. Approximate token figures use about four text bytes per token; image model-token cost is unknown. Pin a transcript message by its current 1-based number or a source by its ID, for example `/context pin 3` or `/context pin source-2`. Use `/context pins` to review pins and `/context unpin <selector>` to remove one. Pinned items are preserved by automatic optimization even if their combined size exceeds the configured text budget.

`/context compact [objective]` asks the active model for a restart summary, displays it, and asks before applying it. Accepting saves the full transcript as the parent session and starts a compacted child session. The summary may omit details and source bodies. Load the parent with `/load <parent_session_id> --full` to restore the original transcript; plain `/load <parent_session_id>` may restore its storage-optimized copy when one exists.

`/sources` lists source references retained in the conversation. `/sources compare source-1 source-2` compares two explicitly selected sources; it refuses the comparison and reports the source if an excerpt cannot be isolated unambiguously. `/sources audit` asks the active model to classify claims in its last answer as supported, contradicted, or unconfirmed using available conversation context. Audits are not independent verification. `/sources save <name>` and `/sources load <name>` save or reload references to local files and URLs; use `/sources list` and `/sources delete <name>` to manage saved sets. Reloading is explicit and reports unavailable resources. Source bundles contain references rather than cached copies. Source labels are provenance hints, not verified citations. Default panels hide full local paths; Markdown conversation exports include source URIs and can contain local paths.

`/branch [name]` saves the active conversation and switches to a child session. The original remains independently available. `/sessions` also offers a Branch action for a saved session. `/retry [instruction]` retries the last completed chat exchange with the currently selected model and replaces its answer only after a successful response. A retry instruction is added to the retained user turn. `/notes add <text>`, `/notes show`, and `/notes clear` manage local notes attached to the session; notes are not sent to the model unless you include them in a prompt. Notes are included in an explicit Markdown conversation export.

### Manage saved sessions

`/history` shows the current chat transcript. Use `/sessions` to manage saved conversation archives. An optional query filters by title, tag, model, session ID, or the first user message:

```text
/sessions release
```

Choose a session to open it, rename it, replace its comma-separated tags, toggle its favorite status, or delete it. Titles allow up to 100 characters; each of at most 10 tags allows 24. Leaving a tag list empty clears it. Delete displays the selected title and ID and requires a separate confirmation before removing that archive.

The dashboard offers the same rename, tag, favorite, and delete controls on its Conversations page. It shows that page only when **Show Conversations** is enabled in `/config` under **Dashboard Settings**; a configured dashboard password also protects these actions. Dashboard deletion likewise asks for confirmation. Session metadata stays in the local archive, and the existing retention policy still removes old sessions automatically.

### Add context to a message

Use `--` to separate a context command from the question about that context. Combine `/file`, `/url`, and `/search` with `&&` to gather several sources before asking:

```text
/search Go concurrency && /url https://go.dev/doc/ && /file notes.md -- Compare these notes with the official guide.
```

`/library` is a standalone command and cannot be placed in a `&&` chain. It can load a library and send a prompt in one command:

```text
/library load project-notes -- Summarize the open questions.
```

`/speak`, `/image`, `/coordinate`, and `/prompt` also run on their own. They cannot be chained with `&&`.

### Coordinate models for one question

Run `/coordinate` to open a framed menu. Choose **Chain**, **Independent**, or **Debate**, then review its two prefilled agents and suggested roles. You can select two or three models and edit each agent's role. The default roles match the selected algorithm; model and role choices are saved separately for each strategy, so switching strategies restores that strategy's preset. Each valid model, role, algorithm, debate-round, or execution change is saved immediately to the local `config.json`, even if you leave the menu before arming a question. Debate uses one round by default and allows at most two. The summary shows planned Duck.ai requests, excluding retries, and the maximum number of simultaneous requests. These are request-count estimates, not price or latency promises. If a saved model is no longer available, replace it before arming the next question. Repeated models are allowed without an advisory. Reusing a role across different models shows an advisory that the agents may give similar answers.

After selecting **Use for next question**, type your question normally. A question submitted with `/file`, `/url`, `/search`, `/library`, or a loaded `/prompt` can also use the selection. `/image` uses its separate generation flow and leaves the selection pending. Run `/coordinate cancel` to clear a pending selection.

Every agent sees the current question and conversation context. **Independent** asks the first two agents to answer separately, then asks a synthesis agent to combine their responses. **Chain** asks agent 1 for a draft, agent 2 to critique it, and the final agent to write the response. **Debate** has agents 1 and 2 exchange revisions for the selected one or two rounds, then a synthesis agent writes the response. With two selected models, agent 1 also performs the synthesis; with three, agent 3 does it. The CLI shows the current stages in the spinner and retains a colored progress panel for terminal redraw after resizing.

Requests run one after another by default. In the **Execution** submenu, choose **Parallel independent steps** for Independent or Debate to overlap the first two answers and, in Debate, each pair of revisions. Synthesis still waits for both responses. Chain has dependencies between every stage, so it always stays sequential. Parallel execution is opt-in and is saved with the preset.

Chain and Independent plan three Duck.ai requests. Debate plans five requests with one round or seven with two rounds, whether sequential or parallel. Retries performed by Duck.ai's existing request recovery can add calls. If a stage fails, choose **Resume incomplete stages** to retry failed/cancelled work and continue its dependents while keeping completed outputs. Each Resume action attempts each incomplete stage once; Duck.ai's existing `Retry-After`, clean-session, and browser recovery remains active inside each request. If at least one agent output is complete and another is missing, you can instead choose **Synthesize partial result**. That answer is labeled `Coordinate · partial`, names unavailable stages, and is summarized as using available outputs only. Choose **Stop** to leave the conversation unchanged.

Cancelling with Ctrl+C also stops the run without committing the question or answer. A complete answer, or a partial answer you explicitly selected, is added to the conversation exactly once. After an accepted answer, the CLI asks whether to save that run's prompts and intermediate outputs. **Do not save** is the default; choosing **Cancel** also leaves the report unsaved. Non-interactive runs keep this no-save behavior because they cannot make a per-run choice.

Choosing **Save report** stores the question, strategy, stage order/status, model and role assignments, prompts, outputs, errors, and final answer inside the compressed local conversation archive. The report follows the conversation's retention period and is removed when that conversation is deleted. Reports are excluded from ordinary chat messages, later model context, and analytics. Use `/coordinate runs` to list saved reports in the active conversation and `/coordinate show <run_id>` to inspect one, including its intermediate prompts and outputs. These records are model output history; they do not independently verify the claims.

After the response, a compact **Coordinate results** panel lists each model and role, its completed/incomplete stages, whether all planned outputs were used, and that synthesis was asked to flag unresolved disagreements. This is a summary of the workflow; it does not independently verify model claims. The CLI then announces that Coordinate mode has ended.

If the question includes an image, every selected model must support image input. If a model is incompatible, the CLI does not send the question and keeps the image and coordinate selection. Reopen `/coordinate` to adjust the models, then submit the question again.

### Saved prompts

Run `/prompt` to open the prompt menu, or use a subcommand directly:

```text
/prompt list
/prompt add concise -- Answer in a few sentences.
/prompt edit concise -- Give a short answer with one example.
/prompt load concise
/prompt remove concise
```

## Models

Choose a model with `/model`, or pass its ID with `--model` in one-shot mode. Model availability and names come from Duck.ai and can change.

| Name | Model ID |
| --- | --- |
| GPT-6 Luna | `gpt-6-luna` |
| GPT-5.6 Luna | `gpt-5.6-luna` |
| GPT-5.4 Nano | `gpt-5.4-nano` |
| GPT-5.4 Mini | `gpt-5.4-mini` |
| Claude Haiku 4.5 | `claude-haiku-4-5` |
| Mistral Small 4 | `mistral-small-2603` |
| GPT OSS 120B | `tinfoil/gpt-oss-120b` |
| Gemma 4 31B | `tinfoil/gemma4-31b` |

The CLI accepts shorter aliases for some models. `/model` shows the names you can select.

## Settings and appearance

Open `/config` to change settings. Changes are saved as you make them. The menu groups settings by area and supports arrow-key navigation, Enter to select, and typing to filter.

Choose **Reset all settings to defaults** at the bottom of the menu to replace the complete configuration file with built-in defaults. The confirmation defaults to **No** and requires typing `RESET`. This clears the API key, dashboard password, saved prompts, coordinate presets, and terms acceptance; it also stops active API/dashboard services. The default 90-day retention is reapplied immediately, so older saved conversations and dashboard history may be removed. Exported files are not deleted, and the terms prompt appears again on the next launch.

Appearance options include the `official`, `retro`, `mono`, `dracula`, and `nord` themes, plus framed model responses. Choose **Full terminal** or a maximum frame width of 80, 96, 112, 128, 144, or 160 columns, then choose **Single** or **Heavy** border lines. Frames use the available terminal width up to the selected maximum; the default is a 112-column maximum with single lines. The settings apply to responses, panels, `/help`, and framed menus. Dumb terminals keep ASCII borders. You can also set the default model, export folder, global prompt, saved prompts, search behavior, library folders, tray shortcuts, voice and playback options, dashboard, and API options.

## Search, files, and images

`/search <query>` runs a web search and adds the results to the conversation. Add `-- <question>` to ask about the results immediately. Duck.ai's built-in web search is a separate optional feature. In `/config`, open **Duck.ai Native Tools** to let Duck.ai decide when a prompt needs current web results.

`/file` accepts text files, text-based PDFs, and PNG, JPEG, or WebP images. Ordinary files, PDF input, and extracted PDF text are each limited to 10 MiB. PDF text is labeled by page (for example, `[Page 3]`) so answers can refer to the original page. `/library` can load PDFs too and keeps its existing 2 MiB aggregate context limit. For example:

```text
/file src/main.go -- Explain the request flow.
/file docs/report.pdf -- Summarize the findings and identify the relevant pages.
/file photos/diagram.png -- Describe this diagram.
```

PDF import extracts embedded text only. It does not run OCR, so scanned PDFs with no embedded text cannot be used. Password-protected, malformed, empty-text, and oversized PDFs produce an error before any context is added.

When an image is added without a prompt, it is attached to your next message. Image input support depends on the selected Duck.ai model. If that model cannot accept images, the CLI uses GPT-5.4 Mini for that request and says so in the chat.

Run `/image` to open a compact, framed menu for the image description, style, format, and optional reference image. Each setting opens its own submenu. Review the prompt before generating, or return to the menu to adjust the choices.

Pass an idea directly to skip the guided choices and review the generated prompt:

```text
/image A small red bird on a snowy branch at sunrise
```

`/image` temporarily enables Duck.ai's native image tool for that request. To let regular chat prompts use native image generation too, turn it on in `/config` under **Duck.ai Native Tools**. Generated files are saved in the `images` folder inside your export directory.

## Voice chat and shortcuts

Run `/speak` to open or restore a compact Chrome or Chromium window. By default, the call starts automatically; allow microphone access when Chromium asks. If you turn off **Auto-start calls**, select **Start** instead. **Mute** toggles the microphone. **End** finishes the current call and leaves the retry screen open. **Close** closes the voice window. **Minimize** hides it in the tray while the session continues.

The voice window displays user and assistant subtitles. Starting `/speak` from an active text chat carries up to the 24 most recent user and assistant text messages (48 KiB maximum) into the voice conversation. Completed voice transcripts are added in conversation order to the same saved CLI chat when you close the voice window, so you can continue in text with the shared context. Incomplete assistant replies are left out. A session stores at most 128 transcript messages and 700 KiB for safe local transfer; an individual transcript is limited to 64 KiB. The CLI warns if a transcript exceeds these limits and later turns continue to be saved. Images and source metadata are not sent as voice context. Under `/config` → **Audio Agent Speak**, choose the Duck.ai voice, window size, start muted, subtitle visibility, output volume, connection sound, and auto-start behavior.

After an interactive launch, DuckChat can keep a background service in the system tray. Use its menu to open a text chat, restore voice chat, edit shortcuts, or quit the service. The defaults are `Ctrl+Alt+D` for text and `Ctrl+Alt+V` for voice. Change them in `/config` or from the tray menu. Shortcuts depend on the desktop and may require operating-system permissions.

One-shot prompts and `--help` or `--version` do not start the tray service. Choose **Quit DuckChat** in the tray menu to stop the background service.

## Local usage dashboard

The dashboard is optional and listens on `127.0.0.1`. Use these commands from the CLI:

```text
/dashboard on
/dashboard open
/dashboard status
/dashboard off
```

In `/config`, **Dashboard Settings** controls autostart, the port, retention, and privacy options. Conversation lists and prompt/response text are hidden by default. You can set a password in the same menu.

The Activity page shows up to 200 recent CLI events while the process is running. It keeps them in memory and does not write an activity log. Prompt and response text appear only when **Show Conversation Content** is enabled. Turning it off clears retained text. Conversation analysis is a separate opt-in action. Before excerpts are sent to Duck.ai for a report, the dashboard shows the sessions and estimated token count included.

The dashboard stores usage snapshots and saved conversation archives locally. Token counts are estimates, not Duck.ai billing totals. Retention and conversation visibility are controlled in `/config`.

## Local API

The optional HTTP API runs on `127.0.0.1` by default. Enable it in `/config`, then use `/api` to start or stop the server. The Swagger UI is available at `http://127.0.0.1:8080/doc/index.html` while the API is running. `POST /api/v1/chat` returns its existing JSON envelope. `POST /api/v1/chat/stream` accepts the same JSON request and returns server-sent events; use `curl -N` to keep the stream open:

```sh
curl -N http://127.0.0.1:8080/api/v1/chat/stream \
  -H 'Content-Type: application/json' \
  -d '{"message":"Explain the Go memory model"}'
```

The stream begins with `start` (`{"model":"..."}`), followed by one or more `delta` events (`{"delta":"..."}`). Join the delta strings to rebuild the response. A successful stream ends with `done`, containing `model`, `message_id`, and `metadata`; a failed generation ends with `error`, containing an API error `code`, `message`, and `details`. Invalid requests are returned as the normal JSON error envelope before streaming starts. The same API key and configured CORS rules apply. If the client disconnects, generation is cancelled and the partial turn is removed from chat history.

The API requires an API key before it can bind to a non-loopback address. Keep it on the local address unless a remote integration needs access and you have configured a key.

## Updates and troubleshooting

The CLI checks GitHub Releases when it starts and can install updates with `/update`. Use `/update --force` to skip the confirmation prompt.

For a report, run `/issue`. It opens a short form and passes the details to a new GitHub issue page. Before reporting a connection problem, you can try `/clear` to start a fresh local chat session. If Duck.ai returns a rate limit, the CLI resets its isolated session data and retries once. If the retry is refused, wait before trying again.

If chat or voice cannot start, check that Chrome or Chromium is installed and available on your `PATH`. To cancel a request that is taking too long, press Ctrl-C once. Press it again at an idle prompt to exit.

## Local data and privacy

Configuration, conversation archives, and usage snapshots are kept in the operating system's user data folders. The export folder defaults to `Documents/duckchat` in your home directory. You can change it in `/config`.

The dashboard stays on the local machine by default. It hides conversation text and conversation lists until you enable those options. An AI conversation report sends sampled excerpts to Duck.ai only after you request the report. Voice audio and subtitles are not saved in the text conversation history.

DuckDuckGo AI Chat CLI is an unofficial client. It is not affiliated with or endorsed by DuckDuckGo. Follow DuckDuckGo's terms when using Duck.ai, and check important answers against reliable sources.
