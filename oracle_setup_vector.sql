-- oracle_setup_vector.sql
-- Schema migration v2: Oracle AI Vector Search for semantic memory
--
-- Prerequisites:
--   1. Base schema from oracle_setup.sql already applied (sessions + messages tables)
--   2. ALL_MINILM_L6_V2 ONNX model loaded into the database (see below)
--   3. GRANT DB_DEVELOPER_ROLE TO hermes;  (or GRANT CREATE MINING MODEL)
--
-- Loading the ONNX embedding model (run once as hermes user):
--   BEGIN
--     DBMS_VECTOR.LOAD_ONNX_MODEL(
--       'DEMO_PY_DIR',                    -- directory object pointing to model file
--       'all_MiniLM_L6_v2.onnx',          -- filename
--       'ALL_MINILM_L6_V2',               -- model name in DB
--       JSON('{"function":"embedding","embeddingOutput":"embedding","input":{"input":["DATA"]}}')
--     );
--   END;
--   /
--
-- Alternatively, if the model is already loaded in another schema, grant access:
--   GRANT SELECT ON MINING_MODEL ALL_MINILM_L6_V2 TO hermes;

-- Add embedding column to messages table
ALTER TABLE messages ADD (
    embedding VECTOR(384, FLOAT32)
);

-- Create vector index for fast approximate nearest-neighbor search (HNSW)
CREATE VECTOR INDEX idx_messages_embedding ON messages(embedding)
    ORGANIZATION NEIGHBOR PARTITIONS
    DISTANCE COSINE
    WITH TARGET ACCURACY 95;

-- Update schema version
UPDATE schema_version SET version = 2 WHERE version = 1;
COMMIT;
