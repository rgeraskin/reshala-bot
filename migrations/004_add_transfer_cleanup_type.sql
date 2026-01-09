-- Add 'transfer' as valid cleanup_type for session transfers between chats
-- SQLite doesn't support ALTER TABLE to modify CHECK constraints, so we recreate the table

-- Step 1: Create new table with updated constraint
CREATE TABLE IF NOT EXISTS cleanup_log_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id TEXT NOT NULL,
    cleanup_type TEXT NOT NULL CHECK(cleanup_type IN ('expired', 'manual', 'error', 'transfer')),
    messages_deleted INTEGER DEFAULT 0,
    tools_deleted INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Step 2: Copy existing data
INSERT INTO cleanup_log_new (id, chat_id, cleanup_type, messages_deleted, tools_deleted, created_at)
SELECT id, chat_id, cleanup_type, messages_deleted, tools_deleted, created_at
FROM cleanup_log;

-- Step 3: Drop old table
DROP TABLE cleanup_log;

-- Step 4: Rename new table
ALTER TABLE cleanup_log_new RENAME TO cleanup_log;

-- Note: Index on claude_session_id already exists from migration 002
