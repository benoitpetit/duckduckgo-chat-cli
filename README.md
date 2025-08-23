# 🦆 DuckDuckGo AI Chat CLI

<p align="center">
  <img src="logobig.png" width="800" alt="DuckDuckGo AI Chat CLI Logo">
  <br>
  <strong>🚀 A powerful CLI tool to interact with DuckDuckGo's AI</strong><br>
  <em>Advanced context integration, multi-models and enhanced productivity</em><br>
  <em>🧠 Now with Intelligent Analytics, Context Optimization & Persistent History</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go Version">
  <a href="https://github.com/benoitpetit/duckduckGO-chat-cli/releases" target="_blank">
    <img src="https://img.shields.io/github/v/release/benoitpetit/duckduckGO-chat-cli?style=flat-square" alt="Latest Release">
  </a>
  <a href="[https://github.com/benoitpetit/duckduckGO-chat-cli#platforms](https://github.com/benoitpetit/duckduckGO-chat-cli?tab=readme-ov-file#-installation)" target="_blank">
    <img src="https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20MacOS-blue?style=flat-square" alt="Platform">
  </a>
  <img src="https://img.shields.io/badge/License-Open%20Source-green?style=flat-square" alt="License">
  <a href="https://liberapay.com/devbyben" target="_blank">
    <img src="https://img.shields.io/liberapay/patrons/devbyben.svg?logo=liberapay&label=Donators&color=gold&style=flat-square" alt="Liberapay Donators">
  </a>
</p>


<p align="center">
  <a href="#-key-features">Features</a> •
  <a href="#-installation">Installation</a> •
  <a href="#-usage">Usage</a> •
  <a href="#-configuration">Configuration</a> •
  <a href="#-intelligent-features-deep-dive">Intelligent Features</a> •
  <a href="reverse/README.md">Reverse Engineering</a>
</p>

---

## ✨ Key Features

### 💬 Chat Experience
- **🔄 Real-time streaming** - Live response display with smooth markdown formatting
- **🤖 Multiple AI models** - GPT-4o mini, GPT-5 mini, Claude 3 Haiku, Llama 3.3, Mistral Small, o4-mini & more
- **💻 Terminal-native** - Optimized for command-line workflows with interactive menus
- **⌨️ Smart autocompletion** - Interactive command menus and context-aware suggestions
- **🔑 Auto-authentication** - Seamless session management with dynamic header refresh
- **🔄 Model switching** - Interactive model selection during conversations

### 🧠 Intelligent Features ✨ **NEW**
- **📊 Smart Analytics** - Real-time session statistics with API monitoring, performance metrics, and usage insights
- **🎯 Context Optimization** - Automatic context compression and importance scoring to maintain conversation quality
- **💾 Persistent History** - Intelligent session management with compression, recovery, and searchable archive
- **📈 Performance Tracking** - Monitor success rates, error patterns, token usage, and optimization effectiveness

### 🧠 Context Integration
- **🔍 Web search** - Integrate DuckDuckGo search results into conversations
- **📄 File processing** - Add local file content (15+ formats: Go, Python, JS, TS, JSON, MD, etc.)
- **🌐 URL scraping** - Extract and analyze webpage content with Chrome-based scraping
- **🚀 Project analysis** - Generate comprehensive project prompts with PMP auto-installation
- **💾 Session persistence** - Maintain conversation history across sessions
- **📚 Library management** - Organize and search through document collections
- **⛓️ Command Chaining** - Chain multiple commands (e.g., `/url`, `/file`, `/search`) using `&&` to build a combined context before sending a final prompt with `--`.

### 🛠️ Productivity Tools
- **📋 Smart clipboard** - Copy responses, code blocks, or full conversations with interactive selection
- **📤 Advanced export** - Save conversations in multiple formats with search-based filtering
- **📝 History management** - Browse your conversation history with intelligent search
- **🔍 Content search** - Search within conversations and document libraries
- **⚙️ Interactive config** - Visual configuration menus for all settings
- **🎨 Rich formatting** - Colored output with markdown rendering
- **⚡ Performance** - Efficient memory usage and fast response times

### 🌐 API Server
- **🚀 REST API** - Built-in HTTP server for external integrations
- **📡 Real-time endpoints** - Chat, history, and status endpoints
- **🔧 Request logging** - Configurable API request/response logging
- **📖 Auto-documentation** - Interactive API documentation at root endpoint

### 📚 Library System
- **📁 Document collections** - Organize files into searchable libraries
- **🔍 Advanced search** - Search across all libraries with pattern matching
- **📊 Library stats** - File counts, sizes, and modification dates
- **🎯 Selective loading** - Load specific libraries or files into context
- **📁 Multi-format support** - 15+ file formats automatically recognized

### 🔧 Advanced Features
- **🛠️ PMP Integration** - Auto-install and use Prompt My Project for code analysis
- **🔄 Dynamic headers** - Automatic browser session management
- **📱 Cross-platform** - Linux, Windows, macOS support

### ⛓️ Command Chaining
- **🚀 Chain multiple commands** - Execute a series of commands in a single line using `&&`
- **💡 Context accumulation** - Combine context from files, URLs, and web searches
- **🗣️ Final prompt** - Use `--` to add a final prompt to the accumulated context for the AI to process

```bash
# Chain multiple commands to build a rich context before asking a question
You: /url https://devbyben.fr/about && /search devbyben.fr twitter account && /file ~/Documents/my_notes.md -- Based on all this, write a summary.
```

## 🧠 Intelligent Features Deep Dive

### 📊 Session Analytics & Statistics
The CLI now tracks comprehensive real-time metrics:

- **Performance Metrics**: API call timing, success/failure rates, retry counts
- **Content Analysis**: Message counts, token estimation, context optimization savings  
- **Error Tracking**: 418/429 error monitoring, VQD refresh rates, header refresh frequency
- **Usage Patterns**: Command usage statistics, model changes, file/URL processing
- **Session Duration**: Total time spent in the session, average response times

**Commands:**
- `/stats` - View current session analytics anytime
- Automatic display on `/exit` with detailed session summary

### 🎯 Smart Context Optimization
Automatically manages conversation context for optimal performance:

- **Intelligent Compression**: Compresses old context when approaching token limits
- **Importance Scoring**: Preserves critical information while removing redundant content
- **Memory Efficiency**: Reduces token usage by up to 40% while maintaining quality
- **Configurable Thresholds**: Customize optimization triggers and compression ratios
- **Automatic Context Management**: Automatically compresses and optimizes context as needed

### 💾 Persistent History Management
Never lose important conversations:

- **Session Persistence**: Automatically saves conversations with metadata
- **Compression Storage**: Efficient gzip compression reduces storage by 70%
- **Session Recovery**: Resume conversations from any previous session
- **Intelligent Indexing**: Fast search and retrieval of historical conversations
- **Searchable Archive**: Use `/history` to browse and search past conversations
- **Session Loading**: Load previous sessions interactively or by ID with `/load`

## 🤖 Available Models

| Model Name         | Integration ID                            | Alias          | Strength                 | Best For                 | Characteristics                            |
| :----------------- | :---------------------------------------- | :------------- | :----------------------- | :----------------------- | :----------------------------------------- |
| **GPT-5-Mini**     | gpt-5-mini                                | gpt-5-mini     | Adaptive problem-solving | Multimodal reasoning     | • Context-aware<br>• Efficient with nuance |
| **GPT-4o mini**    | gpt-4o-mini                               | gpt-4o-mini    | General purpose          | Everyday questions       | • Fast<br>• Well-balanced                  |
| **Claude 3 Haiku** | claude-3-haiku-20240307                   | claude-3-haiku | Creative writing         | Explanations & summaries | • Clear responses<br>• Concise             |
| **Llama 3.3 70B**  | meta-llama/Llama-3.3-70B-Instruct-Turbo   | llama          | Programming              | Code-related tasks       | • Technical precision<br>• Detailed        |
| **Mistral Small**  | mistralai/Mistral-Small-24B-Instruct-2501 | mixtral        | Knowledge & analysis     | Complex topics           | • Reasoning<br>• Logic-focused             |
| **o4-mini**        | o4-mini                                   | o4mini         | Speed                    | Quick answers            | • Very fast<br>• Compact responses         |

## 📦 Installation

> [📥 **Download Latest Release**](https://github.com/benoitpetit/duckduckGO-chat-cli/releases/latest)

### 🚀 1. Direct Download & Run

<details>
<summary><strong>🪟 Windows (PowerShell)</strong></summary>

```powershell
$exe="duckduckgo-chat-cli_windows_amd64.exe"; Invoke-WebRequest -Uri ((Invoke-RestMethod "https://api.github.com/repos/benoitpetit/duckduckGO-chat-cli/releases/latest").assets | Where-Object name -like "*windows_amd64.exe").browser_download_url -OutFile $exe; Start-Process -Wait -NoNewWindow -FilePath ".\$exe"
```

</details>

<details>
<summary><strong>🐧 Linux (curl)</strong></summary>

```bash
curl -LO $(curl -s https://api.github.com/repos/benoitpetit/duckduckGO-chat-cli/releases/latest | grep -oP 'https.*linux_amd64' | grep -oP 'https.*v[0-9]+\.[0-9]+\.[0-9]+_linux_amd64' | head -1) && chmod +x duckduckgo-chat-cli_v*_linux_amd64 && ./duckduckgo-chat-cli_v*_linux_amd64
```

</details>

<details>
<summary><strong>🍎 MacOS (curl)</strong></summary>

<br/>
<strong>Apple Silicon (ARM64):</strong>

```bash
curl -LO $(curl -s https://api.github.com/repos/benoitpetit/duckduckGO-chat-cli/releases/latest | grep -oP 'https.*darwin_arm64' | grep -oP 'https.*v[0-9]+\.[0-9]+\.[0-9]+_darwin_arm64' | head -1) && chmod +x duckduckgo-chat-cli_v*_darwin_arm64 && ./duckduckgo-chat-cli_v*_darwin_arm64
```

<strong>Intel (AMD64):</strong>

```bash
curl -LO $(curl -s https://api.github.com/repos/benoitpetit/duckduckGO-chat-cli/releases/latest | grep -oP 'https.*darwin_amd64' | grep -oP 'https.*v[0-9]+\.[0-9]+\.[0-9]+_darwin_amd64' | head -1) && chmod +x duckduckgo-chat-cli_v*_darwin_amd64 && ./duckduckgo-chat-cli_v*_darwin_amd64
```

</details>

### 🔨 2. Build from source

**📋 Prerequisites:**
- Go 1.21+ (`go version`)
- Chrome/Chromium 115+ (`chromium-browser --version`)

```sh
git clone https://github.com/benoitpetit/duckduckGO-chat-cli
cd duckduckGO-chat-cli
./scripts/build.sh
```

## 🎯 Usage

### 📖 Typical Workflow

<details>
<summary><strong>⛓️ Example 1: Command Chaining</strong></summary>

```bash
# Chain multiple commands to build a rich context before asking a question
You: /url https://devbyben.fr/about && /search devbyben.fr twitter account && /file ~/Documents/my_notes.md -- Based on all this, write a summary.
```
</details>

<details>
<summary><strong>🔍 Example 2: Code Analysis</strong></summary>

```bash
./duckduckgo-chat-cli_linux_amd64
Accept terms? [yes/no] yes
Type /help to show available commands

You: /search Go concurrency patterns -- What are the best practices?
🔍 Searching for: Go concurrency patterns
✅ Added 10 search results to the context
Processing your request about the search results...

You: /file main.go -- Explain this code and suggest improvements
📄 Adding file content: main.go
✅ Successfully added content from file: main.go
Processing your request about the file...

GPT-4o mini: Based on the search results about Go concurrency patterns and your code...
[Detailed analysis follows]

You: /stats
🧠 SESSION ANALYTICS SUMMARY
═══════════════════════════════════════════════════════════
📊 Session Performance
   Duration: 8.5m | Messages: 6 | Avg Response: 1.1s
   API Success Rate: 100% (3/3 calls)
═══════════════════════════════════════════════════════════

You: /copy
Choose what to copy:
1) Last Q&A exchange
2) Largest code block
3) Cancel
Enter your choice: 2
✅ Content copied to clipboard
```

</details>

<details>
<summary><strong>🗂️ Example 3: Session Loading</strong></summary>

```bash
# List and interactively select a previous session to restore
You: /load

# Or load a session directly by its ID
You: /load 12345

# The chat context and history will be restored for continued conversation.
```
</details>

### 📝 Command Reference

| Command           | Example                  | Description                     |
| ----------------- | ------------------------ | ------------------------------- |
| 🔍 `/search <query> [-- prompt]` | `/search machine learning -- What are the best practices?`   | Add search results as context and optionally process them with a prompt   |
| 📁 `/file <path> [-- prompt]`    | `/file src/main.go -- Explain this code`      | Import file content as context and optionally analyze it with a prompt  |
| 📚 `/library [command] [args]`   | `/library add /path/to/docs` | Manage library directories for bulk file operations |
| 🌐 `/url <link> [-- prompt]`     | `/url github.com/golang -- Summarize this page` | Add webpage content as context and optionally process it with a prompt  |
| 📦 `/pmp [path] [options] [-- prompt]` | `/pmp . -i "*.go" -e "test/*"` | Generate structured project prompts with automatic PMP installation |
| 📝 `/prompt` or `/prompt add <name> -- <prompt>` | `/prompt` or `/prompt add myprompt -- This is my prompt` | Manage and load custom prompts. `/prompt` opens the interactive menu; subcommands are also available. |
| 📊 `/stats` ✨    | `/stats`                 | Show real-time session analytics and performance metrics |
| 📡 `/api [port]`         | `/api` or `/api 8080`    | Start or stop the API server    |
| 🤖 `/model`          | `/model` or `/model 2`   | Change AI model (interactive)   |
| 🧹 `/clear`          | `/clear`                 | Reset conversation context (with session save) |
| 📤 `/export`         | `/export`                | Export content (interactive)    |
| 📋 `/copy`           | `/copy`                  | Copy to clipboard (interactive) |
| 📚 `/history`        | `/history`               | Display conversation history    |
| 📚 `/load [session_id]` | `/load` or `/load 12345` | Load and restore a previous session interactively or by ID |
| ⚙️ `/config`         | `/config`                | Modify configuration settings   |
| 🏷️ `/version`        | `/version`               | Show version and system info    |
| 🔄 `/update`         | `/update` or `/update --force` | Update the CLI to the latest version |
| ❓ `/help`           | `/help`                  | Show available commands         |
| 🚪 `/exit`           | `/exit`                  | Exit application (with analytics) |

### 📝 Prompt Management

- `/prompt` : Open the interactive prompt management menu (list, add, edit, remove)
- `/prompt list` : List all saved prompts
- `/prompt add <name> -- <prompt>` : Add a new prompt
- `/prompt edit <name> -- <prompt>` : Edit an existing prompt
- `/prompt remove <name>` : Remove a prompt
- `/prompt load <name>` : Send the prompt content as a message to the model

> **Note:** `/prompt` is not chainable and does not support chaining with `&&`. The `--` is only for separating the prompt text, not for chaining.

## ⚙️ Configuration

### 🎛️ Application Settings

| Option           | Description               | Default              | Range              |
| ---------------- | ------------------------- | -------------------- | ------------------ |
| `DefaultModel`   | Starting AI model         | gpt-4o-mini          | 5 models available |
| `GlobalPrompt`   | System prompt always sent | ""                   | Any text           |
| `ExportDir`      | Export directory          | ~/Documents/duckchat | Any valid path     |
| `ShowMenu`       | Display commands on start | true                 | true/false         |
| `AnalyticsEnabled` ✨ | Enable session analytics | true                 | true/false         |

### 🔍 Search Settings

| Option           | Description               | Default | Range      |
| ---------------- | ------------------------- | ------- | ---------- |
| `MaxResults`     | Results per search        | 10      | 1-20       |
| `IncludeSnippet` | Show result descriptions  | true    | true/false |

### 📚 Library Settings

| Option           | Description               | Default | Range      |
| ---------------- | ------------------------- | ------- | ---------- |
| `Enabled`        | Enable library system     | true    | true/false |
| `Directories`    | List of library paths     | []      | Array of paths |

### 📡 API Settings

| Option        | Description               | Default | Range           |
|---------------|---------------------------|---------|-----------------|
| `Enabled`     | Enable API server         | `false` | `true`/`false`  |
| `Port`        | API server port           | `8080`  | Any valid port  |
| `Autostart`   | Start API on app launch   | `false` | `true`/`false`  |

> 💡 **Tip:** Use `/config` to modify these settings interactively.

## 🔄 Auto-Update System

The CLI includes an integrated update system that keeps your installation current:

### 📥 Update Features

- **🔍 Automatic Check:** Checks for new versions every 24 hours at startup
- **🔐 SHA256 Verification:** Verifies downloaded binaries for security
- **🎯 Cross-Platform:** Works on Linux, Windows, and macOS
- **⚡ In-Place Update:** Updates the current binary without changing location
- **🔄 Backup & Restore:** Creates backups and restores on failure

### 🎯 Usage

```bash
# Check for updates and install (with confirmation)
/update

# Force update without confirmation
/update --force

# The CLI will also prompt you when updates are available:
🆕 A new version is available!
   Current: 1.1.9
   Latest:  1.2.0
💡 Run '/update' to update to the latest version.
```

### 🔧 Update Process

1. **Detection:** Detects your OS and architecture automatically
2. **Download:** Downloads the correct binary from GitHub releases
3. **Verification:** Verifies SHA256 checksum for security
4. **Installation:** Replaces the current binary with the new version
5. **Restart:** Prompts you to restart the CLI to use the new version

## 🛠️ Development & Contributing

### 🚀 Automated Release Process

This project uses GitHub Actions for automated building and releasing:

- **Development:** Work on the `master` branch
- **Release:** Create PR to `prod` branch to trigger automatic release
- **CI/CD:** Automated testing, building, and publishing

### 📚 Development Documentation

- **[🔧 CI/CD & Release Process](.github/README.md)** - Complete GitHub Actions documentation
- **[🔬 Reverse Engineering](reverse/README.md)** - Complete technical reverse engineering documentation

## 🚨 Troubleshooting

### 🔧 Connection Issues

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

## 📜 License & Ethics

### 🛡️ Privacy & Responsibility

- **Privacy First:** This tool respects your privacy and stores no personal data
- **Verify Information:** Always verify critical information from AI responses
- **Responsible Use:** Use responsibly and in accordance with DuckDuckGo's terms

---

*🔧 This is an unofficial client and not affiliated with or endorsed by DuckDuckGo*

<p align="center">
  <table width="100%"">
    <tr>
      <td align="center" style="border: 1px solid #6ed04c; padding: 20px;">
        <strong>Made with ♥ for the community</strong>
        <br>
        <img src="logo.png" width="150" alt="DuckDuckGo AI Chat CLI Logo">
      </td>
    </tr>
  </table>
</p>
