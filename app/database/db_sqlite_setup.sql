-- https://kerkour.com/sqlite-for-servers
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = 1000000000;
PRAGMA temp_store = memory;

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS crawl_queue(
    id INTEGER PRIMARY KEY,
    source TEXT NOT NULL,
    url TEXT NOT NULL,
    status INTEGER DEFAULT 0, -- Pending
    depth INTEGER,
    addedAt TEXT DEFAULT CURRENT_TIMESTAMP,
    updatedAt TEXT DEFAULT CURRENT_TIMESTAMP,
    isRefresh INTEGER DEFAULT 0
) STRICT;

-- This table temporarily stores referrers before the referenced page is crawled. Then, the relationship is stored in `pages_referrers`.
CREATE TABLE IF NOT EXISTS crawl_queue_referrers(
  queueItem INTEGER NOT NULL,
  referrer INTEGER NOT NULL,
  FOREIGN KEY(queueItem) REFERENCES crawl_queue(id) ON DELETE CASCADE,
  FOREIGN KEY(referrer) REFERENCES pages(id) ON DELETE CASCADE
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS crawl_queue_referrers_src_dest_unique ON crawl_queue_referrers(queueItem, referrer);

-- When a canonical page is removed from the queue, also remove all pages that link to it
CREATE TRIGGER IF NOT EXISTS queue_delete_followers_on_canonical_delete AFTER DELETE ON crawl_queue BEGIN
  DELETE FROM crawl_queue WHERE source = old.source AND url IN (SELECT url FROM canonicals WHERE canonical = old.url);

  -- This should be enforced by the foreign key, but in some cases, the child rows don't get deleted automatically for some reason, even when PRAGMA foreign_keys = ON.
  DELETE FROM crawl_queue_referrers WHERE queueItem = old.id;
END;

-- When a canonical URL is discovered, it is cached in this table to prevent excessively querying the target
CREATE TABLE IF NOT EXISTS canonicals(
    id INTEGER PRIMARY KEY,
    source TEXT NOT NULL,
    url TEXT NOT NULL,
    canonical TEXT NOT NULL,
    crawledAt TEXT DEFAULT CURRENT_TIMESTAMP
) STRICT;

-- After a page is crawled, it is added to this table
CREATE TABLE IF NOT EXISTS pages(
    id INTEGER PRIMARY KEY,
    source TEXT NOT NULL,

    crawledAt TEXT DEFAULT CURRENT_TIMESTAMP,
    depth INTEGER NOT NULL,
    errorInfo TEXT,
    status INTEGER NOT NULL,

    url TEXT NOT NULL,
    title TEXT,
    description TEXT,
    content TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS pages_referrers(
  source INTEGER NOT NULL,
  dest INTEGER NOT NULL,
  FOREIGN KEY(source) REFERENCES pages(id) ON DELETE CASCADE,
  FOREIGN KEY(dest) REFERENCES pages(id) ON DELETE CASCADE
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS pages_referrers_src_dest_unique ON pages_referrers(source, dest);

CREATE TRIGGER IF NOT EXISTS pages_disallow_update_id AFTER UPDATE ON pages
WHEN old.id != new.id BEGIN
  -- A page's ID should be read-only
  SELECT RAISE(FAIL, 'Updating a page ID is not allowed');
END;

-- Ensure a page can only be queued and/or indexed once per source and that pages can only have one canonical per source
CREATE UNIQUE INDEX IF NOT EXISTS queue_source_url ON crawl_queue(source, url COLLATE nocase);
CREATE UNIQUE INDEX IF NOT EXISTS page_source_url ON pages(source, url COLLATE nocase);
CREATE UNIQUE INDEX IF NOT EXISTS canonical_source_url ON canonicals(source, url COLLATE nocase);

-- Create a full-text search table
CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
    url,
    title,
    description,
    content,

    -- Specify that this FTS table is contentless and gets its content from the `pages` table
    content=pages
);

-- When a page is deleted, delete its canonicals too
CREATE TRIGGER IF NOT EXISTS delete_page_canonicals_on_page_delete AFTER DELETE ON pages BEGIN
  DELETE FROM canonicals WHERE source = old.source AND canonical = old.url;
END;

-- When a page is inserted, delete non-canonical versions.
-- For example, if a page gets indexed, and then the site owner changes its canonical, and then the page gets refreshed,
-- refreshing will cause a canonical and a page with the new URL to be inserted. When this happens, the old entry should be deleted.
CREATE TRIGGER IF NOT EXISTS delete_page_non_canonicals_on_page_insert BEFORE INSERT ON pages BEGIN
  DELETE FROM pages WHERE source = new.source AND url IN (SELECT url FROM canonicals WHERE source = new.source AND canonical = new.url);
END;

-- Use triggers to automatically sync the FTS table with the content table
-- https://sqlite.org/fts5.html#external_content_tables
CREATE TRIGGER IF NOT EXISTS pages_auto_insert AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, url, title, description, content) VALUES (new.rowid, new.url, new.title, new.description, new.content);
  -- Remove relevant crawl queue entries if they exist
  DELETE FROM crawl_queue WHERE source = new.source AND url = new.url;
  DELETE FROM crawl_queue WHERE source = new.source AND url IN (SELECT url FROM canonicals WHERE canonical = new.url);
END;

CREATE TRIGGER IF NOT EXISTS pages_auto_delete AFTER DELETE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, url, title, description, content) VALUES('delete', old.rowid, old.url, old.title, old.description, old.content);
END;

CREATE TRIGGER IF NOT EXISTS pages_auto_update AFTER UPDATE ON pages BEGIN
  INSERT INTO pages_fts(pages_fts, rowid, url, title, description, content) VALUES('delete', old.rowid, old.url, old.title, old.description, old.content);
  INSERT INTO pages_fts(rowid, url, title, description, content) VALUES (new.url, new.title, new.description, new.content);
  -- Remove crawl queue entry if it exists
  DELETE FROM crawl_queue WHERE source = new.source AND url = new.url;
END;

CREATE TABLE IF NOT EXISTS vec_chunks(
  id INTEGER PRIMARY KEY,
  page INTEGER NOT NULL,
  chunk TEXT NOT NULL,
  chunkIndex INTEGER NOT NULL,
  FOREIGN KEY(page) REFERENCES pages(id) ON DELETE CASCADE
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS vec_chunks_page_chunk_unique ON vec_chunks(page, chunkIndex);

CREATE TABLE IF NOT EXISTS embed_queue(
  id INTEGER PRIMARY KEY,
  page INTEGER NOT NULL,
  chunk TEXT NOT NULL,
  chunkIndex INTEGER NOT NULL,
  status INTEGER NOT NULL DEFAULT 0, -- Pending
  addedAt TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updatedAt TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(page) REFERENCES pages(id) ON DELETE CASCADE
) STRICT;

CREATE UNIQUE INDEX IF NOT EXISTS embed_queue_page_chunk_unique ON embed_queue(page, chunkIndex);
