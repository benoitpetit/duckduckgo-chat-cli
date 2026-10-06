# DuckDuckGo AI Chat CLI

<p align="center">
  <img src="logobig.png" width="800" alt="DuckDuckGo AI Chat CLI Logo">
  <br>
  <strong>A powerful CLI tool to interact with DuckDuckGo's AI</strong><br>
  <em>Advanced context integration, multi-models and enhanced productivity</em><br>
  <em>Local usage dashboard, intelligent analytics, context optimization & persistent history</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go Version">
  <a href="https://github.com/benoitpetit/duckduckgo-chat-cli/releases" target="_blank">
    <img src="https://img.shields.io/github/v/release/benoitpetit/duckduckgo-chat-cli?style=flat-square" alt="Latest Release">
  </a>
  <a href="https://github.com/benoitpetit/duckduckgo-chat-cli?tab=readme-ov-file#installation" target="_blank">
    <img src="https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20MacOS-blue?style=flat-square" alt="Platform">
  </a>
  <img src="https://img.shields.io/badge/License-Open%20Source-green?style=flat-square" alt="License">
  <a href="https://liberapay.com/devbyben" target="_blank">
    <img src="https://img.shields.io/liberapay/patrons/devbyben.svg?logo=liberapay&label=Donators&color=gold&style=flat-square" alt="Liberapay Donators">
  </a>
</p>

<p align="center">
  <a href="#key-features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#usage">Usage</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#intelligent-features-deep-dive">Intelligent Features</a> •
  <a href="reverse/README.md">Reverse Engineering</a>
</p>

---

## Key Features

### Chat Experience

- **Real-time streaming** - Live response display with smooth markdown formatting
- **Multiple AI models** - GPT-5.6 Luna, GPT-5.4 Nano and Mini, Claude Haiku 4.5, Mistral Small 4 & more
- **Terminal-native** - Optimized for command-line workflows with interactive menus
- **Smart autocompletion** - Interactive command menus and context-aware suggestions
- **Auto-authentication** - Seamless session management with dynamic header refresh
- **Model switching** - Interactive model selection during conversations

### Intelligent Features **NEW**

- **Smart Analytics** - Local session statistics with response performance, errors, estimated tokens, and context usage
- **Context Optimization** - Automatic context compression and importance scoring to maintain conversation quality
- **Persistent History** - Intelligent session management with compression, recovery, and searchable archive
- **Performance Tracking** - Monitor success rates, error patterns, token usage, and optimization effectiveness

### Context Integration

- **Web search** - Integrate DuckDuckGo search results into conversations
- **Native Duck.ai tools** - Optional native Web Search with source citations and image generation, with a loading indicator during browser authentication
- **File processing** - Add bounded local text files (Go, Python, JS, TS, JSON, Markdown, and similar formats)
- **URL scraping** - Extract and analyze webpage content with Chrome-based scraping
- **Session persistence** - Maintain conversation history across sessions
- **Library management** - Organize and search through document collections
- **Command Chaining** - Chain multiple commands (e.g., `/url`, `/file`, `/search`) using `&&` to build a combined context before sending a final prompt with `--`.

### Productivity Tools

- **Smart clipboard** - Copy responses, code blocks, or full conversations with interactive selection
- **Advanced export** - Save conversations in multiple formats with search-based filtering
- **History management** - Browse your conversation history with intelligent search
- **Content search** - Search within conversations and document libraries
- **Interactive config** - Visual configuration menus for all settings
- **Rich formatting** - Colored output with markdown rendering
- **Performance** - Efficient memory usage, bounded network operations, and fast response times
- **Cancellation** - Press Ctrl-C once to cancel an active request without closing the CLI

### API Server

- **REST API** - Built-in HTTP server for external integrations
- **Real-time endpoints** - Chat, history, and status endpoints
- **Request logging** - Configurable API request/response logging
- **Auto-documentation** - Interactive API documentation at root endpoint
- **Safe local default** - Listens on `127.0.0.1`; configure an API key before exposing it beyond the local machine

### Library System

- **Document collections** - Organize files into searchable libraries
- **Advanced search** - Search across all libraries with pattern matching
- **Library stats** - File counts, sizes, and modification dates
- **Selective loading** - Load specific libraries or files into context
- **Text-file support** - Bounded, validated local text files with clear binary-file errors

### Advanced Features

- **Dynamic headers** - Automatic browser session management
- **Cross-platform** - Linux, Windows, macOS support

### Command Chaining

- **Chain multiple commands** - Execute a series of commands in a single line using `&&`
- **Context accumulation** - Combine context from files, URLs, and web searches
- **Final prompt** - Use `--` to add a final prompt to the accumulated context for the AI to process

```bash
# Chain multiple commands to build a rich context before asking a question
You: /url https://devbyben.fr/about && /search devbyben.fr twitter account && /file ~/Documents/my_notes.md -- Based on all this, write a summary.
```

## Intelligent Features Deep Dive

### Session Analytics & Statistics

The CLI now tracks comprehensive real-time metrics:

- **Performance Metrics**: Chat response timing, success/failure rates, and per-model observations
- **Content Analysis**: Message counts, token estimation, context optimization savings
- **Error Tracking**: 418/429 error monitoring, VQD refresh rates, header refresh frequency
- **Usage Patterns**: Command usage statistics, model changes, file/URL processing
- **Session Duration**: Total time spent in the session, average response times

**Commands:**

- `/stats` - View current session analytics anytime
- Automatic display on `/exit` with detailed session summary

### Smart Context Optimization

Automatically manages conversation context for optimal performance:

- **Intelligent Compression**: Compresses old context when approaching token limits
- **Importance Scoring**: Preserves critical information while removing redundant content
- **Memory Efficiency**: Reduces token usage by up to 40% while maintaining quality
- **Configurable Thresholds**: Customize optimization triggers and compression ratios
- **Automatic Context Management**: Automatically compresses and optimizes context as needed

### Persistent History Management

Never lose important conversations:

- **Session Persistence**: Automatically saves conversations with metadata
- **Compression Storage**: Efficient gzip compression reduces storage by 70%
- **Session Recovery**: Resume conversations from any previous session
- **Intelligent Indexing**: Fast search and retrieval of historical conversations
- **Searchable Archive**: Use `/history` to browse and search past conversations
- **Session Loading**: Load previous sessions interactively or by ID with `/load`

## Available Models

| Model Name           | Integration ID   | Alias            | Strength             | Best For                 | Characteristics                     |
| :------------------- | :--------------- | :--------------- | :------------------- | :----------------------- | :---------------------------------- |
| **GPT-5.6 Luna**     | gpt-5.6-luna     | gpt-5.6-luna     | General purpose      | Everyday questions       | • Fast<br>• Well-balanced           |
| **GPT-5.4 Nano**     | gpt-5.4-nano     | gpt-5.4-nano     | Lightweight tasks    | Quick answers            | • Fast<br>• Efficient               |
| **GPT-5.4 Mini**     | gpt-5.4-mini     | gpt-5.4-mini     | Speed                | Quick answers            | • Very fast<br>• Compact responses  |
| **Claude Haiku 4.5** | claude-haiku-4-5 | claude-haiku-4-5 | Creative writing     | Explanations & summaries | • Clear responses<br>• Concise      |
| **Mistral Small 4**  | mistral-small-2603 | mistral-small-4  | Knowledge & analysis | Complex topics           | • Reasoning<br>• Logic-focused      |
| **GPT OSS 120B**     | tinfoil/gpt-oss-120b | gpt-oss-120b     | Programming          | Code-related tasks       | • Technical precision<br>• Detailed |
| **Gemma 4 31B**      | tinfoil/gemma4-31b | gemma-4-31b      | Lightweight tasks    | Short answers            | • Efficient<br>• Compact responses  |

## Installation

> [Download Latest Release](https://github.com/benoitpetit/duckduckgo-chat-cli/releases/latest)

### 1. Direct Download & Run

<details>
<summary><strong> Windows (PowerShell)</strong></summary>

```powershell
$exe="duckduckgo-chat-cli_windows_amd64.exe"; Invoke-WebRequest -Uri ((Invoke-RestMethod "https://api.github.com/repos/benoitpetit/duckduckgo-chat-cli/releases/latest").assets | Where-Object name -like "*windows_amd64.exe").browser_download_url -OutFile $exe; Start-Process -Wait -NoNewWindow -FilePath ".\$exe"
```

</details>

<details>
<summary><strong> Linux (curl)</strong></summary>

```bash
curl -LO $(curl -s https://api.github.com/repos/benoitpetit/duckduckgo-chat-cli/releases/latest | grep -oP 'https.*linux_amd64' | grep -oP 'https.*v[0-9]+\.[0-9]+\.[0-9]+_linux_amd64' | head -1) && chmod +x duckduckgo-chat-cli_v*_linux_amd64 && ./duckduckgo-chat-cli_v*_linux_amd64
```

</details>

<details>
<summary><strong> MacOS (curl)</strong></summary>

<br/>
<strong>Apple Silicon (ARM64):</strong>

```bash
curl -LO $(curl -s https://api.github.com/repos/benoitpetit/duckduckgo-chat-cli/releases/latest | grep -oP 'https.*darwin_arm64' | grep -oP 'https.*v[0-9]+\.[0-9]+\.[0-9]+_darwin_arm64' | head -1) && chmod +x duckduckgo-chat-cli_v*_darwin_arm64 && ./duckduckgo-chat-cli_v*_darwin_arm64
```

<strong>Intel (AMD64):</strong>

```bash
curl -LO $(curl -s https://api.github.com/repos/benoitpetit/duckduckgo-chat-cli/releases/latest | grep -oP 'https.*darwin_amd64' | grep -oP 'https.*v[0-9]+\.[0-9]+\.[0-9]+_darwin_amd64' | head -1) && chmod +x duckduckgo-chat-cli_v*_darwin_amd64 && ./duckduckgo-chat-cli_v*_darwin_amd64
```

</details>

### 2. Build from source

**Prerequisites:**

- Go 1.24+ (`go version`)
- Chrome/Chromium 115+ (`chromium-browser --version`)

The chat backend is Duck.ai. Chrome or Chromium is used headlessly to obtain the rotating browser proof required by Duck.ai; no account credentials are stored by the CLI. See the [protocol notes](reverse/README.md) for the current request flow.

```sh
git clone https://github.com/benoitpetit/duckduckgo-chat-cli
cd duckduckgo-chat-cli
./scripts/build.sh
```

## Usage

### Typical Workflow

<details>
<summary><strong> Example 1: Command Chaining</strong></summary>

```bash
# Chain multiple commands to build a rich context before asking a question
You: /url https://devbyben.fr/about && /search devbyben.fr twitter account && /file ~/Documents/my_notes.md -- Based on all this, write a summary.
```

</details>

<details>
<summary><strong> Example 2: Code Analysis</strong></summary>

```bash
./duckduckgo-chat-cli_linux_amd64
Accept terms? [yes/no] yes
Type /help to show available commands

You: /search Go concurrency patterns -- What are the best practices?
 Searching for: Go concurrency patterns
 Added 10 search results to the context
Processing your request about the search results...

You: /file main.go -- Explain this code and suggest improvements
 Adding file content: main.go
 Successfully added content from file: main.go
Processing your request about the file...

GPT-5.6 Luna: Based on the search results about Go concurrency patterns and your code...
[Detailed analysis follows]

You: /stats
 SESSION ANALYTICS SUMMARY
═══════════════════════════════════════════════════════════
 Session Performance
   Duration: 8.5m | Messages: 6 | Avg Response: 1.1s
   Chat Success Rate: 100% (3/3 requests)
═══════════════════════════════════════════════════════════

You: /copy
Choose what to copy:
1) Last Q&A exchange
2) Largest code block
3) Cancel
Enter your choice: 2
 Content copied to clipboard
```

</details>

<details>
<summary><strong> Example 3: Session Loading</strong></summary>

```bash
# List and interactively select a previous session to restore
You: /load

# Or load a session directly by its ID
You: /load session_12345

# The chat context and history will be restored for continued conversation.
```

</details>

### Command Reference

| Command                                       | Example                                                    | Description                                                                                           |
| --------------------------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `/search <query> [-- prompt]`                 | `/search machine learning -- What are the best practices?` | Add search results as context and optionally process them with a prompt                               |
| `/file <path> [-- prompt]`                    | `/file src/main.go -- Explain this code`                   | Import file content as context and optionally analyze it with a prompt                                |
| `/library [command] [args]`                   | `/library add /path/to/docs`                               | Manage library directories for bulk file operations                                                   |
| `/url <link> [-- prompt]`                     | `/url github.com/golang -- Summarize this page`            | Add webpage content as context and optionally process it with a prompt                                |
| `/prompt` or `/prompt add <name> -- <prompt>` | `/prompt` or `/prompt add myprompt -- This is my prompt`   | Manage and load custom prompts. `/prompt` opens the interactive menu; subcommands are also available. |
| `/stats`                                      | `/stats`                                                   | Show current CLI session analytics and performance metrics                                            |
| `/dashboard <action>`                         | `/dashboard on`                                            | Start or stop the local usage dashboard, or show its status (`on`, `off`, `status`)                   |
| `/api [port]`                                 | `/api` or `/api 8080`                                      | Start or stop the API server                                                                          |
| `/model`                                      | `/model` or `/model 2`                                     | Change AI model (interactive)                                                                         |
| `/clear`                                      | `/clear`                                                   | Reset conversation context (with session save)                                                        |
| `/export`                                     | `/export`                                                  | Export content (interactive)                                                                          |
| `/copy`                                       | `/copy`                                                    | Copy to clipboard (interactive)                                                                       |
| `/history`                                    | `/history`                                                 | Display conversation history                                                                          |
| `/load [session_id]`                          | `/load` or `/load session_12345`                           | Load and restore a previous session interactively or by ID                                            |
| `/config`                                     | `/config`                                                  | Modify configuration settings                                                                         |
| `/version`                                    | `/version`                                                 | Show version and system info                                                                          |
| `/update`                                     | `/update` or `/update --force`                             | Update the CLI to the latest version                                                                  |
| `/help`                                       | `/help`                                                    | Show available commands                                                                               |
| `/exit`                                       | `/exit`                                                    | Exit application (with analytics)                                                                     |

### Prompt Management

- `/prompt` : Open the interactive prompt management menu (list, add, edit, remove)
- `/prompt list` : List all saved prompts
- `/prompt add <name> -- <prompt>` : Add a new prompt
- `/prompt edit <name> -- <prompt>` : Edit an existing prompt
- `/prompt remove <name>` : Remove a prompt
- `/prompt load <name>` : Send the prompt content as a message to the model

> **Note:** `/prompt` is not chainable and does not support chaining with `&&`. The `--` is only for separating the prompt text, not for chaining.

## Configuration

### Application Settings

| Option         | Description                                                      | Default              | Range              |
| -------------- | ---------------------------------------------------------------- | -------------------- | ------------------ |
| `DefaultModel` | Starting AI model                                                | gpt-5.6-luna         | 7 models available |
| `GlobalPrompt` | Instructions prepended to the first message of each conversation | ""                   | Any text           |
| `ExportDir`    | Export directory                                                 | ~/Documents/duckchat | Any valid path     |
| `ShowMenu`     | Display commands on start                                        | true                 | true/false         |

### Dashboard Settings

Open `/config` → **Dashboard Settings**. Defaults are shown below; conversation
viewing and conversation analysis are separate opt-in settings.

| Option | Default | Description |
| --- | --- | --- |
| `Autostart` | `false` | Start the loopback dashboard with the CLI |
| `Port` | `8765` | Local dashboard port; the service binds only to `127.0.0.1` |
| `RefreshIntervalSeconds` | `3` | Browser refresh period (1–60 seconds) |
| `RetentionDays` | `90` | Retention for usage snapshots and conversation archives (1–3650 days) |
| `ShowConversations` | `false` | Show archived conversations in the dashboard |
| `AllowConversationAnalysis` | `false` | Permit an explicitly submitted AI report to use selected excerpts |
| `AnalysisTokenBudget` | `8000` | Approximate input-token ceiling for the conversation report |

### Native Duck.ai Tools

Native Web Search and image generation are available as opt-in features because
Duck.ai does not publish a stable tool protocol. Open `/config`, choose
`Duck.ai Native Tools`, and enable the capabilities you want. Web citations are
added to the streamed response. Generated images are decoded from the Duck.ai
response and saved under `<ExportDir>/images`.

The native tools use the same browser bootstrap as regular chat requests, so
Chrome or Chromium is required. If Duck.ai changes its internal event format,
the feature may need a protocol update without affecting regular text chat.

The existing `/search` command remains available as a deterministic local
context command; enabling native Web Search lets Duck.ai decide when a prompt
needs live web results.

| Option                  | Description                 | Default |
| ----------------------- | --------------------------- | ------- |
| `Tools.Enabled`         | Enable native Duck.ai tools | false   |
| `Tools.WebSearch`       | Allow native Web Search     | false   |
| `Tools.ImageGeneration` | Allow image generation      | false   |

### File and image support

`/file` and `/library` currently import local text and source files into the
conversation context. Individual files are limited to 10 MiB and a library
load contributes at most 2 MiB to one request. They do not yet upload PDF or
image attachments through Duck.ai's native attachment protocol. Native image
generation is supported separately as described above.

### Dictation

The CLI does not include microphone capture or voice transcription. Dictation
is intentionally not enabled until a portable audio and transcription path is
validated for terminal use.

### Search Settings

| Option           | Description               | Default | Range      |
| ---------------- | ------------------------- | ------- | ---------- |
| `MaxResults`     | Results per search        | 10      | 1-50       |
| `MaxRetries`     | Retry attempts for search | 3       | 1-10       |
| `RetryDelay`     | Initial retry delay (s)   | 1       | 1-30       |
| `IncludeSnippet` | Show result descriptions  | true    | true/false |

### Library Settings

| Option        | Description           | Default | Range          |
| ------------- | --------------------- | ------- | -------------- |
| `Enabled`     | Enable library system | true    | true/false     |
| `Directories` | List of library paths | []      | Array of paths |

### API Settings

| Option      | Description             | Default     | Range                        |
| ----------- | ----------------------- | ----------- | ---------------------------- |
| `Enabled`   | Enable API server       | `false`     | `true`/`false`               |
| `Host`      | Bind address            | `127.0.0.1` | Local address by default     |
| `Port`      | API server port         | `8080`      | Any valid port               |
| `Autostart` | Start API on app launch | `false`     | `true`/`false`               |
| `APIKey`    | Protect API requests    | empty       | Required for remote exposure |

For safety, the API refuses to bind to a non-loopback address unless an
`APIKey` is configured. Keep the default loopback binding when the API is only
needed locally.

> **Tip:** Use `/config` to modify these settings interactively.

## Auto-Update System

The CLI includes an integrated update system that keeps your installation current:

### Update Features

- **Automatic Check:** Checks for new versions every 24 hours at startup
- **SHA256 Verification:** Verifies downloaded binaries for security
- **Cross-Platform:** Works on Linux, Windows, and macOS
- **In-Place Update:** Updates the current binary without changing location
- **Backup & Restore:** Creates backups and restores on failure

### Usage

```bash
# Check for updates and install (with confirmation)
/update

# Force update without confirmation
/update --force

# The CLI will also prompt you when updates are available:
 A new version is available!
   Current: <installed version>
   Latest:  <available version>
 Run '/update' to update to the latest version.
```

### Update Process

1. **Detection:** Detects your OS and architecture automatically
2. **Download:** Downloads the correct binary from GitHub releases
3. **Verification:** Verifies SHA256 checksum for security
4. **Installation:** Replaces the current binary with the new version
5. **Restart:** Prompts you to restart the CLI to use the new version

## Local Usage Dashboard

The CLI can serve a private usage dashboard from the same process. It listens only on `127.0.0.1` and does not open a browser automatically. `/dashboard on`, `/dashboard off`, and `/dashboard status` control the running service; autostart and dashboard options are configured separately in `/config`.

```text
/dashboard on      Start the dashboard and print its local URL
/dashboard off     Stop the dashboard
/dashboard status  Show whether it is running
```

Usage snapshots and conversation archives are stored locally. Their shared retention defaults to 90 days (up to 100 conversation archives are kept). The conversation list is disabled by default. When it is enabled, the dashboard can search archives, display transcripts, and copy `/load <session_id>` to resume one in the terminal.

The dashboard includes a web reference for CLI commands, session and per-model statistics, and optional AI reports. Metrics reports use aggregate statistics only. Conversation analysis is independent from transcript viewing: **Allow conversation analysis** can authorize an explicit report without turning on the conversation page. Before sending selected excerpts to Duck.ai, the dashboard shows the included session count and estimated input tokens; the default configurable budget is 8,000 estimated tokens. The provider's actual token count can differ.

## Development & Contributing

### Automated Release Process

Build and verification use the same scripts locally and in GitHub Actions:

- **Local build:** `./scripts/build.sh 1.2.3`
- **Pre-release checks:** `./scripts/pre-release-check.sh`
- **GitHub release:** Run **Actions → Build and Release → Run workflow** and provide a version such as `1.2.3`
- **Windows icon:** The Windows `.exe` embeds `logo.png`; Linux and macOS command-line binaries do not carry an application icon.

### Development Documentation

- **[CI/CD workflow](.github/workflows/release.yml)** - Build and release automation
- **[Build and verification](docs/BUILD.md)** - Local build, Windows icon, and pre-release checks
- **[Reverse Engineering](reverse/README.md)** - Complete technical reverse engineering documentation

## Troubleshooting

### Connection Issues

If you encounter connection errors:

```bash
# Try clearing the conversation context to refresh security tokens
/clear

# Check your Chrome/Chromium installation
chromium-browser --version

# Enable debug mode
DEBUG=true ./duckduckgo-chat-cli_linux_amd64

# View session analytics for debugging
/stats
```

If a request is taking too long, press Ctrl-C once to cancel that request and
continue using the CLI. Press Ctrl-C again when the prompt is idle to exit.

## License & Ethics

### Privacy & Responsibility

- **Local storage:** Usage snapshots and, when enabled by the user, conversation archives are stored on the local machine. A conversation report sends selected excerpts to Duck.ai only after the report is explicitly submitted.
- **Verify Information:** Always verify critical information from AI responses
- **Responsible Use:** Use responsibly and in accordance with DuckDuckGo's terms

---

- This is an unofficial client and not affiliated with or endorsed by DuckDuckGo\*

<p align="center">
  <table width="100%"">
    <tr>
      <td align="center" style="border: 1px solid #d0954c; padding: 20px;">
        <strong>Made for the community</strong>
        <br>
        <img src="logo.png" width="200" alt="DuckDuckGo AI Chat CLI Logo">
      </td>
    </tr>
  </table>
</p>
