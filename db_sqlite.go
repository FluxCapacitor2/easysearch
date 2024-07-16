package main

// https://sqlite.org/fts5.html

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteDatabase struct {
	conn *sql.DB
}

func (db *SQLiteDatabase) setup() error {
	{
		// Enable write-ahead logging for improved write performance (https://www.sqlite.org/wal.html)
		_, err := db.conn.Exec("PRAGMA journal_mode=WAL;")

		if err != nil {
			return err
		}
	}

	{
		_, err := db.conn.Exec(`
			CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5 (
				source UNINDEXED,	
				url,
				title,
				description,
				content
			)
		`)

		if err != nil {
			return err
		}
	}

	{
		_, err := db.conn.Exec(`
			CREATE TABLE IF NOT EXISTS pages (
				source TEXT NOT NULL,
				url TEXT NOT NULL UNIQUE,
				crawledAt DATETIME DEFAULT CURRENT_TIMESTAMP,
				depth INTEGER NOT NULL
			)
		`)

		if err != nil {

			return err
		}
	}

	{
		_, err := db.conn.Exec(`
			CREATE TABLE IF NOT EXISTS crawl_queue (
				source TEXT NOT NULL,
				url TEXT NOT NULL UNIQUE,
				status INTEGER,
				depth INTEGER,
				addedAt DATETIME DEFAULT CURRENT_TIMESTAMP,
				updatedAt DATETIME DEFAULT CURRENT_TIMESTAMP
			)
		`)

		if err != nil {
			return err
		}
	}

	return nil
}

func (db *SQLiteDatabase) addDocument(source string, depth int32, url string, title string, description string, content string) (*Page, error) {
	tx, err := db.conn.Begin()

	if err != nil {
		fmt.Printf("Error starting transaction: %v\n", err)
		return nil, err
	}

	// Remove old entries
	{
		_, err := tx.Exec(`
		DELETE FROM crawl_queue WHERE url = ?;
		DELETE FROM pages WHERE url = ?;
		DELETE FROM pages_fts WHERE url = ?;
		`, url, url, url)

		if err != nil {
			fmt.Printf("Error removing old entries: %v\n", err)
			return nil, err
		}
	}

	// Insert new records for the page (to prevent duplicates and record crawl time as a DATETIME) and the FTS entry (for user search queries)
	{
		_, err := tx.Exec("INSERT INTO pages (source, url, depth) VALUES (?, ?, ?)", source, url, depth)
		if err != nil {
			fmt.Printf("Error inserting new page: %v\n", err)
			return nil, err
		}
	}

	result := tx.QueryRow("INSERT INTO pages_fts (source, url, title, description, content) VALUES (?, ?, ?, ?, ?) RETURNING *", source, url, title, description, content)

	// Return the newly-inserted row
	row := &Page{}

	{
		err := result.Scan(&row.source, &row.url, &row.title, &row.description, &row.content)

		if err != nil {
			fmt.Printf("Error scanning inserted row: %v\n", err)
			return nil, err
		}
	}

	{
		err := tx.Commit()
		if err != nil {
			fmt.Printf("Error inserting new FTS entry: %v\n", err)
			return nil, err
		}
	}

	return row, nil
}

func (db *SQLiteDatabase) hasDocument(source string, url string) (*bool, error) {
	cursor := db.conn.QueryRow("SELECT url FROM pages WHERE source = ? AND url = ?", source, url)

	page := &Page{}
	err := cursor.Scan(&page.url)

	exists := true

	if err != nil {
		if err == sql.ErrNoRows {
			exists = false
		} else {
			return nil, err
		}
	}

	return &exists, nil
}

func (db *SQLiteDatabase) search(sources []string, search string) ([]Result, error) {

	// TODO: instead of using <b>bold</b> to surround matches, create a unique token that doesn't appear in the data.
	//       Then, when the query returns results, convert them into a structured representation that includes the highlighted content.
	query := fmt.Sprintf(`SELECT 
			rank,
			url,
			highlight(pages_fts, 1, '<b>', '</b>') title,
			snippet(pages_fts, 2, '<b>', '</b>', '…', 8) description,
			snippet(pages_fts, 3, '<b>', '</b>', '…', 10) content
		FROM pages_fts WHERE source IN (%s) AND pages_fts MATCH ? ORDER BY rank;
		`, strings.Repeat("?, ", len(sources)-1)+"?")

	// Convert the sources (a []string) into a slice of type []any by manually copying each element
	var args []any = make([]any, len(sources)+1)
	for i, src := range sources {
		args[i] = src
	}
	// Add the search term at the end because there can be no more parameters after a `...`
	args[len(args)-1] = search

	rows, err := db.conn.Query(query, args...)

	if err != nil {
		return nil, err
	}

	var results []Result

	for rows.Next() {
		item := &Result{}
		err := rows.Scan(&item.Rank, &item.Url, &item.Title, &item.Description, &item.Match)
		if err != nil {
			return nil, err
		}
		results = append(results, *item)
	}

	return results, nil
}

func (db *SQLiteDatabase) addToQueue(source string, urls []string, depth int32) error {

	// https://news.ycombinator.com/item?id=27482402

	tx, err := db.conn.Begin()

	if err != nil {
		return err
	}

	for _, url := range urls {
		_, err := tx.Exec("REPLACE INTO crawl_queue (source, url, status, depth) VALUES (?, ?, ?, ?)", source, url, Pending, depth)
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (db *SQLiteDatabase) updateQueueEntry(source string, url string, status QueueItemStatus) error {
	// TODO: check for affected rows. if no rows were affected, then the update failed, potentially due to another instance updating the status at the same time.
	_, err := db.conn.Exec("UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE source = ? AND url = ?", status, source, url)
	return err
}

func (db *SQLiteDatabase) getFirstInQueue(source string) (*QueueItem, error) {
	cursor := db.conn.QueryRow("SELECT * FROM crawl_queue WHERE source = ? AND status = ? ORDER BY addedAt LIMIT 1", source, Pending)

	item := &QueueItem{}
	err := cursor.Scan(&item.source, &item.url, &item.status, &item.depth, &item.addedAt, &item.updatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return item, nil
}

func (db *SQLiteDatabase) cleanQueue() error {
	// Clear all Finished queue items
	{
		_, err := db.conn.Exec("DELETE FROM crawl_queue WHERE status = ?", Finished)

		if err != nil {
			return err
		}
	}

	// Mark items that have been Processing for >20 minutes as Pending again
	{
		_, err := db.conn.Exec("UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE status = ? AND unixepoch() - unixepoch(updatedAt) > 20 * 60", Pending, Processing)

		if err != nil {
			return err
		}
	}

	return nil
}

func (db *SQLiteDatabase) queuePagesOlderThan(source string, daysAgo int32) error {
	rows, err := db.conn.Query("SELECT * FROM pages WHERE source = ? AND unixepoch() - unixepoch(crawledAt) > ?", source, daysAgo*86400)

	if err != nil {
		return err
	}

	for rows.Next() {

		row := &Page{}

		err := rows.Scan(&row.source, &row.url, &row.crawledAt, &row.depth)

		if err != nil {
			return err
		}

		{

			err := db.addToQueue(source, []string{row.url}, row.depth)

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func createSQLiteDatabase(fileName string) (*SQLiteDatabase, error) {
	conn, err := sql.Open("sqlite3", fileName)

	if err != nil {
		return nil, err
	}

	return &SQLiteDatabase{conn}, nil
}
