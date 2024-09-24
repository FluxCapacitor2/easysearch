-- https://kerkour.com/sqlite-for-servers
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = 1000000000;
PRAGMA temp_store = memory;

CREATE TABLE IF NOT EXISTS crawl_queue(
    source TEXT NOT NULL,
    url TEXT NOT NULL,
    status INTEGER DEFAULT 0, -- Pending
    depth INTEGER,
    referrer TEXT,
    addedAt TEXT DEFAULT CURRENT_TIMESTAMP,
    updatedAt TEXT DEFAULT CURRENT_TIMESTAMP
) STRICT;

-- When a canonical URL is discovered, it is cached in this table to prevent excessively querying the target
CREATE TABLE IF NOT EXISTS canonicals(
    source TEXT NOT NULL,
    url TEXT NOT NULL,
    canonical TEXT NOT NULL,
    crawledAt TEXT DEFAULT CURRENT_TIMESTAMP
) STRICT;

-- After a page is crawled, it is added to this table
CREATE TABLE IF NOT EXISTS pages(
    source TEXT NOT NULL,

    crawledAt TEXT DEFAULT CURRENT_TIMESTAMP,
    depth INTEGER NOT NULL,
    referrer TEXT,
    errorInfo TEXT,
    status INTEGER NOT NULL,

    url TEXT NOT NULL,
    title TEXT,
    description TEXT,
    content TEXT
) STRICT;

-- Ensure a page can only be queued and/or indexed once per source and that pages can only have one canonical per source
CREATE UNIQUE INDEX IF NOT EXISTS queue_source_url ON crawl_queue(source, url);
CREATE UNIQUE INDEX IF NOT EXISTS page_source_url ON pages(source, url);
CREATE UNIQUE INDEX IF NOT EXISTS canonical_source_url ON canonicals(source, url);

-- Create a full-text search table
CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
    url,
    title,
    description,
    content,

    -- Specify that this FTS table is contentless and gets its content from the `pages` table
    content=pages
);

-- Use triggers to automatically sync the FTS table with the content table
-- https://sqlite.org/fts5.html#external_content_tables
CREATE TRIGGER IF NOT EXISTS pages_auto_insert AFTER INSERT ON pages BEGIN
  INSERT INTO pages_fts(rowid, url, title, description, content) VALUES (new.rowid, new.url, new.title, new.description, new.content);
  -- Remove crawl queue entry if it exists
  DELETE FROM crawl_queue WHERE source = new.source AND url = new.url;
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
