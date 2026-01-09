-- Add claude_session_id field to track Claude CLI session ID for conversation isolation
ALTER TABLE chat_contexts ADD COLUMN claude_session_id TEXT;

CREATE INDEX IF NOT EXISTS idx_chat_contexts_claude_session_id ON chat_contexts(claude_session_id);
