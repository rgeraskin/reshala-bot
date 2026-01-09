# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AIOps Telegram Bot - A secure Telegram bot that integrates with Claude Code CLI for SRE operations. The bot executes one-shot Claude CLI queries for each Telegram group conversation, providing intelligent assistance for Kubernetes, ArgoCD, Jira, GitHub, Datadog, and other SRE tools.

**Key Architecture Principles:**
- **Per-chat isolation**: Each Telegram group gets its own Claude CLI session with isolated conversation history
- **2-hour session TTL**: Automatic cleanup of inactive sessions to prevent resource leaks
- **Security-first**: Output sanitization to prevent credential leakage, read-only MCP access
- **Group-only access**: Bot ignores private messages by design

## Common Commands

### Development
```bash
# Run locally
go run cmd/bot/main.go

# Run tests
go test ./...

# Run specific package tests
go test ./internal/storage/

# Build binary
go build -o bot cmd/bot/main.go

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o bot-linux cmd/bot/main.go
```

### Docker
```bash
# Build and start
docker-compose up -d

# View logs
docker-compose logs -f bot

# Restart
docker-compose restart bot

# Stop
docker-compose down
```

### Database Operations
```bash
# Initialize database
mkdir -p data
sqlite3 data/bot.db < migrations/001_initial_schema.sql

# Query active contexts
sqlite3 data/bot.db "SELECT chat_id, created_at, expires_at FROM chat_contexts WHERE is_active = 1;"

# View recent messages
sqlite3 data/bot.db "SELECT chat_id, role, created_at FROM messages ORDER BY created_at DESC LIMIT 10;"

# Check tool usage
sqlite3 data/bot.db "SELECT tool_name, COUNT(*) FROM tool_executions GROUP BY tool_name;"
```

### Testing Bot
```bash
# Send message to bot from terminal (requires Telegram user credentials)
./.scripts/tg_send.py @your_bot_username "Test message"

# Send multi-line message
./.scripts/tg_send.py @your_bot_username "What pods are running in production?"

# Read from stdin
echo "Show ArgoCD apps" | ./.scripts/tg_send.py @your_bot_username -
```

**Note**: `tg_send.py` sends messages as your Telegram user account (not as a bot). Requires:
- `TELEGRAM_API_ID` and `TELEGRAM_API_HASH` from https://my.telegram.org
- `TELEGRAM_PHONE` (your phone number with country code)

## Architecture

### Component Hierarchy
```
cmd/bot/main.go (entry point)
    ↓
internal/bot/handler.go (message handler)
    ↓
internal/claude/process.go (SessionManager - executes Claude CLI queries)
    ↓
internal/context/manager.go (session lifecycle)
    ↓
internal/storage/storage.go (SQLite persistence)
```

### Key Components

**Session Management** (`internal/claude/`):
- `SessionManager` tracks active sessions in memory (lightweight, no OS processes)
- Each query executes as a one-shot `claude -p` CLI call
- Concurrency controlled via semaphore on actual query execution (default: 20 concurrent queries)

**Context Lifecycle** (`internal/context/`):
- Background worker runs every 5 minutes to clean up expired sessions (2-hour TTL)
- Context validation ensures queries relate to SRE operations before executing Claude
- Session state tracked in SQLite with `is_active`, `created_at`, `expires_at`

**Platform Abstraction** (`internal/messaging/`):
- `messaging.Platform` interface allows pluggable messaging platforms (Telegram/Slack)
- Telegram-specific implementation in `internal/messaging/telegram/`
- Future Slack integration ready via interface design

**Security** (`internal/security/`):
- Regex-based sanitization of all Claude responses before sending to Telegram
- Patterns detect API keys, tokens, passwords, secrets, JWTs, base64-encoded credentials
- MCP tools configured as read-only (kubectl get/describe/logs, not apply/delete)

### Database Schema

**chat_contexts**: Tracks active Claude sessions per chat
- `chat_id` (PK): Telegram chat/group ID
- `session_id`: UUID for Claude process
- `is_active`: Boolean flag
- `created_at`, `updated_at`, `expires_at`: Timestamps for TTL management

**messages**: Conversation history per chat
- Stores user/assistant messages with timestamps
- Used for context reconstruction if needed

**tool_executions**: Audit log of MCP tool usage
- Tracks which SRE tools were called (kubectl, argocd, jira, etc.)
- Input/output/duration for debugging

**cleanup_log**: Records expired session cleanup
- Audit trail for session lifecycle

## Configuration

### Environment Variables
Required environment variables (set in `.env` or Docker):
- `TELEGRAM_BOT_TOKEN`: Bot token from @BotFather
- `ANTHROPIC_API_KEY`: Claude API key
- `CONFIG_PATH`: Path to config.yaml (optional, defaults to `./configs/config.yaml`)

### config.yaml Structure
- `telegram.allowed_chat_ids`: Whitelist of allowed groups/users (always enforced)
- `claude.cli_path`: Path to claude-code binary
- `claude.project_path`: Claude workspace with MCP servers configured
- `claude.query_timeout`: Per-query timeout (default: 5m)
- `claude.max_concurrent_sessions`: Concurrency limit (default: 20)
- `context.ttl`: Session expiry (default: 2h)
- `context.cleanup_interval`: Cleanup worker interval (default: 5m)
- `security.secret_patterns`: Regex patterns for credential detection

**Config Override**: `configs/config.local.yaml` overrides `config.yaml` for environment-specific settings (not committed).

### Claude Workspace Setup
The bot requires a Claude workspace (`claude.project_path`) with:
1. **`.mcp.json`**: MCP server configurations (Kubernetes, ArgoCD, Jira, GitHub, Datadog, Slack, Telegram)
2. **`.claude/settings.json`**: Read-only tool permissions
3. **`CLAUDE.md`**: Bot instructions and SRE context
4. **`RUNBOOKS.md`**: SRE runbooks and procedures
5. **`RESOURCES.md`**: Dashboards, tools, links

## Development Patterns

### Adding a New MCP Tool
1. Add MCP server to Claude workspace `.mcp.json`
2. Configure read-only permissions in `.claude/settings.json`
3. Restart bot to pick up changes
4. Tool usage automatically logged in `tool_executions` table

### Modifying Context Validation
- Edit `internal/context/validator.go`
- Update keyword matching or implement custom validation logic
- Validation occurs BEFORE spawning Claude process to save resources

### Custom Response Formatting
- Modify `internal/bot/response.go`
- Add formatting for specific message types (code blocks, lists, etc.)
- Responses automatically sanitized by security layer

### Extending to New Platform (e.g., Slack)
1. Implement `messaging.Platform` interface in `internal/messaging/slack/`
2. Add platform-specific client and types
3. Update `cmd/bot/main.go` to instantiate new platform
4. No changes needed to handler/context/storage layers

## Security Considerations

- **Never commit secrets**: Use environment variables, never hardcode API tokens
- **MCP read-only**: All MCP tools must be read-only (no kubectl apply/delete, no write operations)
- **Output sanitization**: All Claude responses pass through security.SanitizeOutput()
- **Group-only access**: Bot validates message type and ignores private chats
- **Session isolation**: Each chat has separate Claude session with isolated conversation history

## Testing

- Unit tests in each package (`*_test.go` files)
- Focus on storage layer (SQLite operations), context manager (TTL logic), security (sanitization patterns)
- Integration tests can spawn test Telegram bot and mock Claude processes

## Troubleshooting

**Bot not responding**: Check `docker-compose logs bot` - likely group permission issue or Claude process spawn failure
**Database locked**: Multiple instances running - ensure only one bot process accesses SQLite
**Context expired**: Normal behavior after 2 hours - send new message to create fresh session
**Claude timeout**: Increase `claude.query_timeout` in config.yaml or check Claude workspace configuration

## Key Implementation Details (Lessons Learned)

### Context Creation and UNIQUE Constraint Issue

**Problem**: The `chat_contexts` table has a UNIQUE constraint on `chat_id`. When a context expired and the bot tried to create a new one, it failed with "Failed to initialize context" because the old row still existed.

**Solution**: Use `INSERT OR REPLACE` in the `CreateContext` function (`internal/storage/chat.go:25`) to replace existing rows instead of failing on duplicates. This allows seamless context recreation after expiry.

```sql
INSERT OR REPLACE INTO chat_contexts (chat_id, chat_type, session_id, ...)
VALUES (?, ?, ?, ...)
```

### Claude CLI Invocation

**Critical**: The Claude CLI does NOT support `--project-path` flag. Instead:

1. Use `cmd.Dir = projectPath` to set the working directory
2. Use `-p --output-format json` flags for one-shot execution
3. Use `--resume <session_id>` to continue a specific session (NOT `--session-id`)

**Session Continuity - Use `--resume` NOT `--session-id`**:

The `--session-id` flag requires exclusive access and fails with "Session ID is already in use" error if any other Claude CLI process is running in the same project directory (e.g., an interactive terminal session). The `--resume` flag works correctly in concurrent scenarios.

```go
// WRONG - fails with "already in use" if another Claude CLI is running
args = append(args, "--session-id", claudeSessionID)

// CORRECT - works with concurrent Claude CLI instances
args = append(args, "--resume", claudeSessionID)
```

**Correct invocation**:
```go
cmd := exec.CommandContext(ctx, cliPath,
    "-p",                    // Print mode (non-interactive)
    "--output-format", "json", // JSON output for parsing
    "--resume", sessionID,   // Continue specific session (NOT --session-id!)
    query,                   // The user query
)
cmd.Dir = projectPath        // Set working directory (NOT --project-path!)
```

### JSON Output Parsing

Claude CLI with `--output-format json` returns a specific structure:

```json
{
  "type": "result",
  "subtype": "success",
  "result": "The actual text response from Claude",
  "session_id": "...",
  "total_cost_usd": 0.35,
  ...
}
```

**Key**: Extract the `result` field, NOT a `content` array. The parser in `internal/claude/process.go:parseClaudeJSON()` handles this correctly.

### Structured Logging with log/slog

The codebase uses Go's standard `log/slog` package (not log.Printf) for structured JSON logging:

**Best practices**:
- Always use key-value pairs: `slog.Info("message", "key", value)`
- Use appropriate levels: `slog.Info`, `slog.Warn`, `slog.Error`, `slog.Debug`
- Include context: `"chat_id"`, `"session_id"`, `"error"` for tracing
- Initialize with JSON handler in main.go for machine-parseable output

**Example**:
```go
slog.Info("Created new context", "chat_id", chatID, "session_id", sessionID)
// Outputs: {"time":"...","level":"INFO","msg":"Created new context","chat_id":"123","session_id":"uuid"}
```

### Session Management Pattern

The bot uses a **lightweight session tracking** pattern:

1. `SessionManager` tracks active sessions in memory (session ID, chat ID, timestamps)
2. No OS processes are spawned for tracking - sessions are just in-memory structs
3. Query execution uses one-shot `claude -p` CLI calls with `--resume <session_id>` for conversation continuity
4. Concurrency is controlled via a channel-based semaphore on actual query execution

This design avoids resource waste from dummy processes while properly controlling concurrency.

### Security Layer

All responses MUST pass through `security.Sanitize()` before sending to Telegram. This:
- Redacts API keys, tokens, passwords using regex patterns
- Logs when sensitive data is detected and removed
- Prevents credential leakage in chat logs

**Pattern examples** (from `internal/security/sanitizer.go`):
- `api[_-]?key[s]?\s*[:=]\s*["']?([^"'\s]+)`
- JWT tokens: `eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`
- Base64 secrets: `[A-Za-z0-9+/]{40,}={0,2}`

---

## Quick Reference: Architecture Internals

### Message Flow
1. **Telegram → Handler** (`internal/messaging/telegram/client.go:93`)
2. **Whitelist check** (`internal/bot/handler.go:61`)
3. **Slash command detection** (`handler.go:71`) - Routes `/new` to cleanup, others to Claude
4. **Context management** (`handler.go:83`) - GetOrCreate session, Refresh TTL
5. **Query validation** (`handler.go:86`) - SRE keywords or slash prefix
6. **Claude execution** (`handler.go:98`) - Get/create session, execute query
7. **Response handling** (`handler.go:110`) - Sanitize, save, send

### Session Lifecycle
- **Creation**: `contextManager.GetOrCreate()` → INSERT OR REPLACE in database
- **Refresh**: Every message extends TTL by 2 hours
- **Expiry**: Background worker checks every 5 minutes
- **Cleanup**: Remove session from memory, delete messages/tools, deactivate context, log audit
- **Manual Reset**: `/new` command triggers `expiryWorker.ManualCleanup()`

### Storage Layer Key Methods
- **CreateContext** (`chat.go:21`): Uses `INSERT OR REPLACE` for UNIQUE constraint handling
- **GetContext** (`chat.go:50`): Returns nil gracefully if not found
- **RefreshContext** (`chat.go:76`): Extends `expires_at`, only if `is_active = 1`
- **GetExpiredContexts** (`chat.go:100`): Query for cleanup worker
- **DeactivateContext** (`chat.go:134`): Sets `is_active = 0`, keeps row
- **DeleteMessagesByChat** (`message.go:68`): Returns count deleted
- **LogCleanup** (`tool.go:68`): Audit trail (types: "expired", "manual", "error")

### Claude CLI Execution
**Command** (`process.go:176`):
```bash
claude-code -p --output-format json --model sonnet --disable-slash-commands [--resume <id>] <query>
```
- **`cmd.Dir = projectPath`** NOT `--project-path` flag
- **One-shot execution**: Each query spawns a new CLI process
- **Concurrency control**: Semaphore limits concurrent queries (not sessions)
- **JSON parsing**: Extracts `result` and `session_id` fields

### Slash Commands
**Detection** (`handler.go:71-81`):
```go
if strings.HasPrefix(msg.Text, "/") {
    switch strings.Fields(msg.Text)[0] {
    case "/new":
        return h.handleNewCommand(msg.ChatID)
    default:
        // Pass through to Claude
    }
}
```

**Adding new commands**:
1. Add case in switch statement
2. Implement `handleXCommand(chatID string) error` method
3. Access handler fields (storage, contextManager, etc.) as needed

### Database Schema
- **chat_contexts**: `chat_id UNIQUE`, enables INSERT OR REPLACE
- **messages**: CASCADE DELETE on chat_id
- **tool_executions**: CASCADE DELETE on chat_id
- **cleanup_log**: No cascade (historical audit)

### Critical File Locations

| Component | File | Purpose |
|-----------|------|---------|
| Main | `cmd/bot/main.go` | Initialization |
| Handler | `internal/bot/handler.go` | Message processing |
| Context Manager | `internal/context/manager.go` | Session lifecycle |
| Expiry Worker | `internal/context/expiry.go` | Cleanup |
| Storage | `internal/storage/chat.go` | Database CRUD |
| Session Manager | `internal/claude/process.go` | Session tracking & CLI execution |
| Validator | `internal/context/validator.go` | Query validation |
