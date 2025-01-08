-- This script creates the embedding tables for one source.

-- Required format string placeholders:
-- 1) source ID (string)
-- 2) vector size (integer)
-- 3-6) (Repeated 4 times) source ID (string)

-- Why use separate tables for each source?
-- * Faster query times when there are many sources with lots of embeddings that aren't included in the user's query
-- * More accurate `k` limit when there are many sources that aren't included in the query
-- * Allows different sources to use different embedding sources with different vector sizes

CREATE VIRTUAL TABLE IF NOT EXISTS pages_vec_%s USING vec0(
  id INTEGER PRIMARY KEY,
  embedding FLOAT[%d] distance_metric=cosine
);

CREATE TRIGGER IF NOT EXISTS pages_refresh_vector_embeddings_%s AFTER UPDATE ON pages
WHEN old.url != new.url OR old.title != new.title OR old.description != new.description OR old.content != new.content BEGIN
  -- If the page has associated vector embeddings, they must be recomputed when the text changes
  DELETE FROM pages_vec_%s WHERE id IN (SELECT page FROM vec_chunks WHERE page = old.id);
END;

CREATE TRIGGER IF NOT EXISTS delete_embedding_on_delete_chunk_%s AFTER DELETE ON vec_chunks BEGIN
  DELETE FROM pages_vec_%s WHERE id = old.id;
END;