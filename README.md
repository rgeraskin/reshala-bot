# AIOps Telegram Bot

A secure Telegram bot that integrates with Claude Code CLI for SRE operations, providing intelligent assistance for Kubernetes, ArgoCD, Jira, GitHub, Datadog, and more.

## Features

- **Claude Code Integration**: Spawns Claude Code CLI processes for each conversation
- **Per-Chat Isolation**: Each Telegram group gets its own isolated session and workspace
- **2-Hour Context Expiry**: Automatic cleanup of inactive sessions after 2 hours
- **Security First**: Output sanitization to prevent credential leakage
- **MCP Server Support**: Pre-configured access to Kubernetes, ArgoCD, Jira, GitHub, Datadog, Slack, and Telegram
- **Context Validation**: Ensures queries relate to SRE operations
- **Group-Only Access**: Bot only responds in Telegram groups/channels (not private messages)
- **Expandable**: Platform abstraction layer ready for future Slack integration

## Prerequisites

- Go 1.22 or higher
- Docker and Docker Compose (for containerized deployment)
- Telegram Bot Token (from [@BotFather](https://t.me/BotFather))
- Anthropic API Key (for Claude Code CLI)
- Configured Claude Code environment (with MCP servers)

## Quick Start

### 1. Create Telegram Bot

1. Open Telegram and search for [@BotFather](https://t.me/BotFather)
2. Send `/newbot` and follow the instructions
3. Save the bot token provided

### 2. Configure Environment

Copy the example environment file:

```bash
cp .env.example .env
```

Edit `.env` and set your credentials:

```bash
TELEGRAM_BOT_TOKEN=your_telegram_bot_token_here
ANTHROPIC_API_KEY=your_anthropic_api_key_here
```

### 3. Update Configuration

Edit `configs/config.yaml`:

```yaml
claude:
  cli_path: "/usr/local/bin/claude-code"
  project_path: "/Users/rg/Projects/work/pashapay/claude"  # Path to your Claude workspace
  query_timeout: "5m"
  max_concurrent_sessions: 20

context:
  ttl: "2h"
  cleanup_interval: "5m"
  validation_enabled: true

storage:
  db_path: "./data/bot.db"
```

### 4. Run with Docker Compose

```bash
# Build and start the bot
docker-compose up -d

# View logs
docker-compose logs -f bot

# Stop the bot
docker-compose down
```

### 5. Add Bot to Group

1. Add your bot to a Telegram group or channel
2. Grant the bot permission to read messages
3. Send a message to test: `What pods are running in production?`

## Development Setup

### Local Development

```bash
# Install dependencies
go mod download

# Run migrations
mkdir -p data
sqlite3 data/bot.db < migrations/001_initial_schema.sql

# Run the bot
go run cmd/bot/main.go
```

### Build Binary

```bash
# Build for your platform
go build -o bot cmd/bot/main.go

# Build for Linux
GOOS=linux GOARCH=amd64 go build -o bot-linux cmd/bot/main.go

# Run the binary
./bot
```

## Project Structure

```
aiops/
├── cmd/
│   └── bot/                    # Main application entry point
├── internal/
│   ├── bot/                    # Message handler and middleware
│   ├── claude/                 # Claude CLI process management
│   ├── context/                # Context lifecycle and validation
│   ├── storage/                # SQLite database layer
│   ├── security/               # Output sanitization
│   ├── messaging/              # Platform abstraction (Telegram/Slack)
│   └── config/                 # Configuration management
├── configs/                    # Configuration files
├── migrations/                 # Database migrations
├── Dockerfile                  # Docker build configuration
├── docker-compose.yml          # Docker Compose setup
└── README.md                   # This file
```

## Configuration

### Bot Configuration

The bot is configured via `configs/config.yaml`:

- **telegram.token**: Telegram bot token (can use env var `${TELEGRAM_BOT_TOKEN}`)
- **claude.cli_path**: Path to claude-code CLI binary
- **claude.project_path**: Path to Claude workspace with MCP servers
- **claude.query_timeout**: Maximum time for a query (default: 5m)
- **claude.max_concurrent_sessions**: Max concurrent chat sessions (default: 20)
- **context.ttl**: Session expiry time after last interaction (default: 2h)
- **context.cleanup_interval**: How often to check for expired sessions (default: 5m)
- **context.validation_enabled**: Whether to validate queries relate to SRE context
- **storage.db_path**: Path to SQLite database file
- **security.secret_patterns**: Regex patterns for credential detection

### Claude Workspace

The bot requires a configured Claude Code environment with:

1. **MCP Servers**: Configured in `.mcp.json`
   - Kubernetes
   - ArgoCD
   - Jira/Confluence (Atlassian)
   - GitHub
   - Datadog
   - Slack
   - Telegram

2. **Context Files**: For query validation
   - `CLAUDE.md`: Bot instructions and context
   - `RUNBOOKS.md`: SRE runbooks and procedures
   - `RESOURCES.md`: Tools, dashboards, and links

3. **Permissions**: Read-only access configured in `.claude/settings.json`

## Usage

### Adding Bot to Group

1. Create or open a Telegram group
2. Add your bot as a member
3. Ensure bot can read messages (group privacy settings)
4. Send a message to the group

### Example Queries

**Kubernetes Operations:**
```
What pods are running in the production namespace?
Show me logs for the payment-api deployment
Describe the nginx-ingress service
```

**ArgoCD:**
```
List all applications in ArgoCD
Show the sync status of payment-service
What resources are managed by the api-gateway app?
```

**Jira:**
```
Show me open incidents
What's the status of PROJ-123?
List issues in the current sprint
```

**Datadog:**
```
Show recent alerts
What monitors are currently firing?
Query logs for errors in the last hour
```

### Bot Behavior

- **Group/Channel Only**: Bot ignores private messages
- **Context Validation**: Rejects unrelated queries with explanation
- **Session Management**: Each group gets isolated conversation context
- **Auto-Expiry**: Sessions expire after 2 hours of inactivity
- **Security**: All responses sanitized to remove credentials

## Security

### Credential Protection

The bot implements multiple security layers:

1. **Output Sanitization**: Regex-based filtering of:
   - API keys
   - Tokens
   - Passwords
   - Secrets
   - Base64-encoded credentials
   - JWT tokens
   - Slack tokens

2. **Read-Only Access**: All MCP tools are configured as read-only
   - kubectl get/describe/logs only
   - Jira/GitHub read operations only
   - No write/delete/modify permissions

3. **Environment Isolation**: Secrets never hardcoded
   - Use environment variables
   - Secrets stored outside workspaces
   - Injected at runtime

4. **Group-Only Access**: Bot only works in groups/channels
   - Prevents private misuse
   - Better audit trail

### Best Practices

1. **Credential Management**:
   - Never commit `.env` files
   - Use secret management tools (Vault, AWS Secrets Manager)
   - Rotate credentials regularly

2. **MCP Configuration**:
   - Use environment variables in `.mcp.json`
   - Don't hardcode API tokens
   - Example:
     ```json
     {
       "mcpServers": {
         "argocd-mcp": {
           "env": {
             "ARGOCD_API_TOKEN": "${ARGOCD_API_TOKEN}"
           }
         }
       }
     }
     ```

3. **Access Control**:
   - Limit bot to specific groups
   - Use Telegram group permissions
   - Monitor bot usage via logs

## Monitoring

### Logs

The bot logs important events:

```bash
# View real-time logs
docker-compose logs -f bot

# View last 100 lines
docker-compose logs --tail=100 bot

# Search logs
docker-compose logs bot | grep ERROR
```

### Database

Inspect the SQLite database:

```bash
# Open database
sqlite3 data/bot.db

# Check active contexts
SELECT chat_id, created_at, expires_at FROM chat_contexts WHERE is_active = 1;

# View recent messages
SELECT chat_id, role, created_at FROM messages ORDER BY created_at DESC LIMIT 10;

# Check cleanup log
SELECT * FROM cleanup_log ORDER BY created_at DESC LIMIT 10;
```

### Metrics

Key metrics to monitor:

- Active chat contexts: `SELECT COUNT(*) FROM chat_contexts WHERE is_active = 1;`
- Messages per chat: `SELECT chat_id, COUNT(*) FROM messages GROUP BY chat_id;`
- Tool executions: `SELECT tool_name, COUNT(*) FROM tool_executions GROUP BY tool_name;`

## Troubleshooting

### Bot Not Responding

1. **Check logs**: `docker-compose logs bot`
2. **Verify bot is running**: `docker-compose ps`
3. **Check chat type**: Bot only works in groups/channels
4. **Test connection**: Send `/start` in the group

### Claude Process Errors

1. **Check claude-code is installed**: `which claude-code`
2. **Verify API key**: `echo $ANTHROPIC_API_KEY`
3. **Check project path**: Ensure workspace exists and is accessible
4. **View process logs**: Check stderr output in container logs

### Context Expired

Normal behavior - sessions expire after 2 hours of inactivity:
- Simply send a new message to create a fresh session
- Previous conversation history is cleaned up automatically

### Database Locked

If you see "database is locked" errors:
1. Check for multiple bot instances running
2. Ensure only one process accesses the database
3. Restart the bot: `docker-compose restart bot`

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/storage/
```

### Adding Features

1. **New MCP Server**:
   - Add to Claude workspace `.mcp.json`
   - Update permissions in `.claude/settings.json`
   - Restart bot to pick up changes

2. **Custom Validation**:
   - Edit `internal/context/validator.go`
   - Add keywords or implement custom logic

3. **Response Formatting**:
   - Modify `internal/bot/response.go`
   - Customize formatting functions

## Future Enhancements

- [ ] Slack integration
- [ ] Prometheus metrics
- [ ] Rate limiting per user
- [ ] Admin commands (`/stats`, `/cleanup`)
- [ ] Multi-language support
- [ ] Voice message support
- [ ] Context export/import

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

MIT License - see LICENSE file for details

## Support

For issues and questions:

- Create an issue on GitHub
- Check existing documentation
- Review logs for error details

## Acknowledgments

- Built with [Claude Code](https://claude.com/claude-code)
- Uses [go-telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api)
- Inspired by SRE best practices
