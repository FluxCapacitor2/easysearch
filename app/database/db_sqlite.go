package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	_ "embed"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteDatabase is a database driver that uses SQLite's FTS5 extension
// to create a full-text search index. For more info on FTS5, see their
// official documentation: https://sqlite.org/fts5.html
type SQLiteDatabase struct {
	conn *sql.DB
}

//go:embed db_sqlite_setup.sql
var setupCommands string

func (db *SQLiteDatabase) Setup() error {
	_, err := db.conn.Exec(setupCommands)
	return err
}

func (db *SQLiteDatabase) AddDocument(source string, depth int32, referrer string, url string, status QueueItemStatus, title string, description string, content string) error {
	_, err := db.conn.Exec("REPLACE INTO pages (source, depth, referrer, status, url, title, description, content) VALUES (?, ?, ?, ?, ?, ?, ?, ?);", source, depth, referrer, status, url, title, description, content)
	return err
}

func (db *SQLiteDatabase) HasDocument(source string, url string) (*bool, error) {
	cursor := db.conn.QueryRow("SELECT 1 FROM pages WHERE source = ? AND (url = ? OR url IN (SELECT canonical FROM canonicals WHERE url = ?));", source, url, url)

	exists := false
	err := cursor.Scan(&exists)

	if err != nil {
		if err == sql.ErrNoRows {
			exists = false
		} else {
			return nil, err
		}
	}

	return &exists, nil
}

type RawResult struct {
	Rank        float64
	URL         string
	Title       string
	Description string
	Content     string
}

var re = regexp.MustCompile(`\W`)

func escape(searchTerm string) string {
	// Split the search term into individual words (this step also removes double quotes from the input)
	words := re.Split(searchTerm, -1)

	var filtered = make([]string, 0, len(words))
	for _, word := range words {
		if len(word) != 0 {
			filtered = append(filtered, word)
		}
	}

	// Surround each word with double quotes and add a * to match partial words at the end of the query
	quoted := fmt.Sprintf("\"%s\"*", strings.Join(filtered, "\" \""))
	return quoted
}

func (db *SQLiteDatabase) Search(sources []string, search string, page uint32, pageSize uint32) ([]Result, *uint32, error) {

	tx, err := db.conn.Begin()
	if err != nil {
		return nil, nil, err
	}

	start := uuid.New().String()
	end := uuid.New().String()

	query := fmt.Sprintf(`
		SELECT 
			pages_fts.rank,
			pages_fts.url,
			highlight(pages_fts, 1, ?, ?) AS title,
			snippet(pages_fts, 2, ?, ?, '…', 8) AS description,
			snippet(pages_fts, 3, ?, ?, '…', 24) AS content
		FROM pages
		JOIN pages_fts ON pages.rowid = pages_fts.rowid
		WHERE pages.source IN (%s)
			AND pages.status = ?
			AND pages_fts MATCH ?
		ORDER BY bm25(pages_fts, 1.0, 3.0, 0.8, 1.0) LIMIT ? OFFSET ?;
		`, strings.Repeat("?, ", len(sources)-1)+"?")

	// Convert the sources (a []string) into a slice of type []any by manually copying each element
	var args []any = make([]any, 0, len(sources)+8)

	// The "start" and "end" tokens are used to highlight search results. They're used in the `highlight` and `snippet` functions in the query.
	args = append(args, start, end, start, end, start, end)

	for _, src := range sources {
		args = append(args, src)
	}

	// Add the required status and search term as parameters
	args = append(args,
		Finished, // (as opposed to the Error or Unindexable states)
		escape(search),
		pageSize,
		(page-1)*pageSize,
	)

	rows, err := db.conn.Query(query, args...)

	if err != nil {
		return nil, nil, err
	}

	var results []Result

	for rows.Next() {
		item := &RawResult{}
		err := rows.Scan(&item.Rank, &item.URL, &item.Title, &item.Description, &item.Content)
		if err != nil {
			return nil, nil, err
		}

		// Process the result to convert strings into `Match` instances
		res := &Result{
			Rank:        item.Rank,
			URL:         item.URL,
			Title:       processResult(item.Title, start, end),
			Description: processResult(item.Description, start, end),
			Content:     processResult(item.Content, start, end),
		}

		results = append(results, *res)
	}

	var total *uint32
	{
		cursor := tx.QueryRow(
			fmt.Sprintf("SELECT COUNT(*) FROM pages JOIN pages_fts ON pages.rowid = pages_fts.rowid WHERE pages.source IN (%s) AND pages.status = ? AND pages_fts MATCH ?", strings.Repeat("?, ", len(sources)-1)+"?"), args[6:8+len(sources)]...)
		err := cursor.Scan(&total)
		if err != nil {
			return nil, nil, err
		}
	}

	{
		err := tx.Commit()
		if err != nil {
			return nil, nil, err
		}
	}

	return results, total, nil
}

// SQLite FTS5 queries support a `highlight` function which surrounds exact matches with strings.
// This function converts the string representation into a struct so that the caller does not have to perform any manual parsing.
func processResult(input string, start string, end string) []Match {

	// Any text between `start` and `end` should be a Match with `highlighted` = true.
	// Any other text should be a Match with `highlighted` = false.

	var matches = make([]Match, 0, 3)

	for {
		startIndex := strings.Index(input, start)

		if len(input) == 0 {
			// Prevent adding an empty match at the end of the list
			return matches
		}

		if startIndex == -1 {
			// `start` wasn't found in the string. Return the entire thing as a nonhighlighted Match.
			matches = append(matches, Match{Highlighted: false, Content: input})
			return matches
		}

		if startIndex > 0 {
			// `start` was found after the beginning of the string.
			matches = append(matches, Match{Highlighted: false, Content: input[0:startIndex]})
			// Trim off the beginning part that we just added as a Match
			input = input[startIndex:]
			continue
		}

		endIndex := strings.Index(input, end)

		if endIndex == -1 {
			// Malformed input; bail
			return matches
		}

		matches = append(matches, Match{Highlighted: true, Content: input[startIndex+len(start) : endIndex]})
		input = input[endIndex+len(end):]
	}
}

func (db *SQLiteDatabase) AddToQueue(source string, referrer string, urls []string, depth int32) error {

	tx, err := db.conn.Begin()

	if err != nil {
		return err
	}

	for _, url := range urls {
		_, err := tx.Exec("INSERT INTO crawl_queue (source, referrer, url, depth) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING;", source, referrer, url, depth)
		if err != nil {
			rbErr := tx.Rollback()
			if rbErr != nil {
				return rbErr
			}
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (db *SQLiteDatabase) PopQueue(source string) (*QueueItem, error) {
	// Find the first item in the queue and update it in one step. If the row isn't returned, another process must have updated it at the same time.
	row := db.conn.QueryRow(`
	  UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE rowid = (
	    SELECT rowid FROM crawl_queue WHERE status = ? AND source = ? ORDER BY addedAt LIMIT 1
	  ) RETURNING source, referrer, url, status, depth, addedAt, updatedAt;
	`, Processing, Pending, source)

	item := &QueueItem{}
	err := row.Scan(&item.Source, &item.Referrer, &item.URL, &item.Status, &item.Depth, &item.AddedAt, &item.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return item, nil
}

func (db *SQLiteDatabase) UpdateQueueEntry(source string, url string, status QueueItemStatus) error {
	if status == Finished {
		// If the item is finished, it can be immediately deleted
		_, err := db.conn.Exec("DELETE FROM crawl_queue WHERE source = ? AND url = ?", source, url)
		return err
	} else {
		_, err := db.conn.Exec("UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE source = ? AND url = ?", status, source, url)
		return err
	}
}

func (db *SQLiteDatabase) CleanQueue() error {
	_, err := db.conn.Exec(`
		-- 1) Clear all Finished queue items (this is a sanity check; they should be immediately deleted from UpdateQueueEntry)
		DELETE FROM crawl_queue WHERE status = ?;

		-- 2) Mark items that have been Processing for >5 minutes as Pending again
		UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE status = ? AND unixepoch() - unixepoch(updatedAt) > 5 * 60;
		`, Finished, Pending, Processing)

	return err
}

func (db *SQLiteDatabase) QueuePagesOlderThan(source string, daysAgo int32) error {
	rows, err := db.conn.Query("SELECT source, referrer, url, crawledAt, depth, status FROM pages WHERE source = ? AND unixepoch() - unixepoch(crawledAt) > ?", source, daysAgo*86400)

	if err != nil {
		return err
	}

	for rows.Next() {

		row := &Page{}

		err := rows.Scan(&row.Source, &row.Referrer, &row.URL, &row.CrawledAt, &row.Depth, &row.Status)

		if err != nil {
			return err
		}

		err = db.AddToQueue(source, row.Referrer, []string{row.URL}, row.Depth)

		if err != nil {
			return err
		}
	}

	return nil
}

func (db *SQLiteDatabase) GetCanonical(source string, url string) (*Canonical, error) {
	cursor := db.conn.QueryRow("SELECT original, canonical, crawledAt FROM canonicals WHERE source = ? AND url = ?", source, url)

	canonical := &Canonical{}
	err := cursor.Scan(&canonical.Original, &canonical.Canonical, &canonical.CrawledAt)

	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func (db *SQLiteDatabase) SetCanonical(source string, url string, canonical string) error {
	_, err := db.conn.Exec("REPLACE INTO canonicals (source, url, canonical) VALUES (?, ?, ?)", source, url, canonical)
	return err
}

func SQLiteFromFile(fileName string) (*SQLiteDatabase, error) {
	conn, err := sql.Open("sqlite3", fileName)

	if err != nil {
		return nil, err
	}

	return &SQLiteDatabase{conn}, nil
}

func SQLite(conn *sql.DB) (*SQLiteDatabase, error) {
	return &SQLiteDatabase{conn}, nil
}
