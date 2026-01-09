-- Add session_id column to messages table for session isolation
-- Existing rows will have NULL session_id (legacy data)
ALTER TABLE messages ADD COLUMN session_id TEXT;

-- Add session_id column to tool_executions table for session isolation
ALTER TABLE tool_executions ADD COLUMN session_id TEXT;

-- Create indexes for efficient session-scoped queries
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tool_executions_session_id ON tool_executions(session_id, created_at);
