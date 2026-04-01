-- oracle_setup.sql
-- Oracle 26ai Free schema for orahermes-agent

-- Create user (run as SYSDBA first):
-- ALTER SESSION SET CONTAINER = FREEPDB1;
-- CREATE USER hermes IDENTIFIED BY <password> DEFAULT TABLESPACE users QUOTA UNLIMITED ON users;
-- GRANT CONNECT, RESOURCE, CTX_APP TO hermes;

-- Sessions table
CREATE TABLE sessions (
    id VARCHAR2(128) PRIMARY KEY,
    source VARCHAR2(32) NOT NULL,
    user_id VARCHAR2(256),
    model VARCHAR2(256),
    model_config CLOB CHECK (model_config IS JSON),
    system_prompt CLOB,
    parent_session_id VARCHAR2(128),
    started_at NUMBER NOT NULL,
    ended_at NUMBER,
    end_reason VARCHAR2(64),
    message_count NUMBER DEFAULT 0,
    tool_call_count NUMBER DEFAULT 0,
    input_tokens NUMBER DEFAULT 0,
    output_tokens NUMBER DEFAULT 0
);

-- Messages table
CREATE TABLE messages (
    id NUMBER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id VARCHAR2(128) NOT NULL REFERENCES sessions(id),
    role VARCHAR2(32) NOT NULL,
    content CLOB,
    tool_call_id VARCHAR2(256),
    tool_calls CLOB CHECK (tool_calls IS JSON),
    tool_name VARCHAR2(256),
    timestamp_val NUMBER NOT NULL,
    token_count NUMBER,
    finish_reason VARCHAR2(64)
);

CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_messages_timestamp ON messages(timestamp_val);

-- Oracle Text index for full-text search on message content
CREATE INDEX idx_messages_content_ft ON messages(content)
    INDEXTYPE IS CTXSYS.CONTEXT
    PARAMETERS ('SYNC (ON COMMIT)');

-- Schema version tracking
CREATE TABLE schema_version (
    version NUMBER PRIMARY KEY,
    applied_at TIMESTAMP DEFAULT SYSTIMESTAMP
);

INSERT INTO schema_version (version) VALUES (1);
COMMIT;
