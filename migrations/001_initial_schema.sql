-- Chat contexts with 2-hour expiry tracking
CREATE TABLE IF NOT EXISTS chat_contexts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL UNIQUE,
    chat_type TEXT NOT NULL,
    session_id TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_interaction TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    is_active BOOLEAN DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_chat_contexts_expires ON chat_contexts(expires_at, is_active);
CREATE INDEX IF NOT EXISTS idx_chat_contexts_chat_id ON chat_contexts(chat_id);

-- Conversation history for context
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (chat_id) REFERENCES chat_contexts(chat_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_messages_chat_id ON messages(chat_id, created_at);

-- Tool execution tracking
CREATE TABLE IF NOT EXISTS tool_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('success', 'error', 'timeout')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (chat_id) REFERENCES chat_contexts(chat_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tool_executions_chat_id ON tool_executions(chat_id, created_at);

-- Cleanup log for audit trail
CREATE TABLE IF NOT EXISTS cleanup_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    cleanup_type TEXT NOT NULL CHECK(cleanup_type IN ('expired', 'manual', 'error')),
    messages_deleted INTEGER DEFAULT 0,
    tools_deleted INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
