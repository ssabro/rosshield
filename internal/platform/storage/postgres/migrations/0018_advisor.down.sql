-- E22-B — 0018 down. 역순 DROP.
DROP INDEX IF EXISTS advisor_tool_calls_turn;
DROP TABLE IF EXISTS advisor_tool_calls;
DROP INDEX IF EXISTS advisor_turns_conversation_seq;
DROP TABLE IF EXISTS advisor_turns;
DROP INDEX IF EXISTS advisor_conversations_tenant_user_updated;
DROP TABLE IF EXISTS advisor_conversations;
