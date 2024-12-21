package database

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	_ "embed"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/fluxcapacitor2/easysearch/app/embedding"
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

//go:embed db_sqlite_embedding.sql
var embedSetupCommands string

func (db *SQLiteDatabase) Setup(ctx context.Context) error {
	_, err := db.conn.ExecContext(ctx, setupCommands)
	return err
}

func (db *SQLiteDatabase) SetupVectorTables(ctx context.Context, sourceID string, dimensions int) error {
	_, err := db.conn.ExecContext(ctx, fmt.Sprintf(embedSetupCommands, sourceID, dimensions, sourceID, sourceID, sourceID, sourceID))
	return err
}

func (db *SQLiteDatabase) DropVectorTables(ctx context.Context, sourceID string) error {
	_, err := db.conn.ExecContext(ctx, fmt.Sprintf(`
	DROP TABLE IF EXISTS pages_vec_%s;
	DROP TRIGGER IF EXISTS pages_refresh_vector_embeddings_%s;
	DROP TRIGGER IF EXISTS delete_embedding_on_delete_chunk_%s;
	`, sourceID, sourceID, sourceID))
	return err
}

func (db *SQLiteDatabase) AddDocument(ctx context.Context, source string, depth int32, referrers []int64, url string, status QueueItemStatus, title string, description string, content string, errorInfo string) (int64, error) {
	id := int64(-1)
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		if err := tx.Rollback(); err != nil {
			return id, err
		}
		return id, err
	}
	err = tx.QueryRowContext(ctx, "REPLACE INTO pages (source, depth, status, url, title, description, content, errorInfo) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING (id);", source, depth, status, url, title, description, content, errorInfo).Scan(&id)
	if err != nil {
		if err := tx.Rollback(); err != nil {
			return id, err
		}
		return id, err
	}

	for _, ref := range referrers {
		_, err = tx.ExecContext(ctx, "INSERT INTO pages_referrers (source, dest) VALUES (?, ?);", ref, id)
		if err != nil {
			if err := tx.Rollback(); err != nil {
				return id, err
			}
			return id, err
		}
	}

	err = tx.Commit()
	if err != nil {
		if err := tx.Rollback(); err != nil {
			return id, err
		}
		return id, err
	}

	return id, err
}

func (db *SQLiteDatabase) AddReferrer(ctx context.Context, source int64, dest int64) error {
	_, err := db.conn.ExecContext(ctx, "INSERT INTO pages_referrers (source, dest) VALUES (?, ?) ON CONFLICT DO NOTHING;", source, dest)
	return err
}

func (db *SQLiteDatabase) RemoveReferrer(ctx context.Context, source int64, dest int64) error {
	_, err := db.conn.ExecContext(ctx, "DELETE FROM pages_referrers WHERE source = ? AND dest = ?;", source, dest)
	return err
}

func (db *SQLiteDatabase) RemoveAllReferences(ctx context.Context, source int64) error {
	_, err := db.conn.ExecContext(ctx, "DELETE FROM pages_referrers WHERE source = ?;", source)
	return err
}

func (db *SQLiteDatabase) GetReferences(ctx context.Context, pageID int64) ([]int64, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT dest FROM pages_referrers WHERE source = ?;", pageID)
	if err != nil {
		return nil, err
	}

	var references []int64
	for rows.Next() {
		var r int64
		err := rows.Scan(&r)
		if err != nil {
			return nil, err
		}
		references = append(references, r)
	}
	return references, nil
}

func (db *SQLiteDatabase) GetReferrers(ctx context.Context, pageID int64) ([]int64, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT source FROM pages_referrers WHERE dest = ?;", pageID)
	if err != nil {
		return nil, err
	}

	var referrers []int64
	for rows.Next() {
		var r int64
		err := rows.Scan(&r)
		if err != nil {
			return nil, err
		}
		referrers = append(referrers, r)
	}
	return referrers, nil
}

func (db *SQLiteDatabase) ListOrphanPages(ctx context.Context) ([]int64, error) {
	rows, err := db.conn.QueryContext(ctx, "SELECT id FROM pages WHERE id NOT IN (SELECT dest FROM pages_referrers);")
	if err != nil {
		return nil, err
	}

	var pageIDs []int64
	for rows.Next() {
		var r int64
		err := rows.Scan(&r)
		if err != nil {
			return nil, err
		}
		pageIDs = append(pageIDs, r)
	}

	return pageIDs, nil
}

func (db *SQLiteDatabase) RemoveDocument(ctx context.Context, source string, url string) error {
	_, err := db.conn.ExecContext(ctx, "DELETE FROM pages WHERE source = ? AND url = ?;", source, url)
	return err
}

func (db *SQLiteDatabase) HasDocument(ctx context.Context, source string, url string) (*bool, error) {
	cursor := db.conn.QueryRowContext(ctx, "SELECT 1 FROM pages WHERE source = ? AND (url = ? OR url IN (SELECT canonical FROM canonicals WHERE url = ?));", source, url, url)

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

func (db *SQLiteDatabase) GetDocument(ctx context.Context, source string, url string) (*Page, error) {
	cursor := db.conn.QueryRowContext(ctx, "SELECT id, source, url, title, description, content, depth, crawledAt, status, errorInfo FROM pages WHERE source = ? AND (url = ? OR url IN (SELECT canonical FROM canonicals WHERE url = ?));", source, url, url)

	page := Page{}
	err := cursor.Scan(&page.ID, &page.Source, &page.URL, &page.Title, &page.Description, &page.Content, &page.Depth, &page.CrawledAt, &page.Status, &page.ErrorInfo)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		} else {
			return nil, err
		}
	}

	return &page, nil
}

func (db *SQLiteDatabase) GetDocumentByID(ctx context.Context, id int64) (*Page, error) {
	cursor := db.conn.QueryRowContext(ctx, "SELECT id, source, url, title, description, content, depth, crawledAt, status, errorInfo FROM pages WHERE id = ?;", id)

	page := Page{}
	err := cursor.Scan(&page.ID, &page.Source, &page.URL, &page.Title, &page.Description, &page.Content, &page.Depth, &page.CrawledAt, &page.Status, &page.ErrorInfo)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		} else {
			return nil, err
		}
	}

	return &page, nil
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

func (db *SQLiteDatabase) Search(ctx context.Context, sources []string, search string, page uint32, pageSize uint32) ([]FTSResult, *uint32, error) {

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
		JOIN pages_fts ON pages.id = pages_fts.rowid
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

	rows, err := db.conn.QueryContext(ctx, query, args...)

	if err != nil {
		return nil, nil, err
	}

	var results []FTSResult

	for rows.Next() {
		item := &RawResult{}
		err := rows.Scan(&item.Rank, &item.URL, &item.Title, &item.Description, &item.Content)
		if err != nil {
			return nil, nil, err
		}

		// Process the result to convert strings into `Match` instances
		res := &FTSResult{
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
		cursor := db.conn.QueryRowContext(
			ctx, fmt.Sprintf("SELECT COUNT(*) FROM pages JOIN pages_fts ON pages.rowid = pages_fts.rowid WHERE pages.source IN (%s) AND pages.status = ? AND pages_fts MATCH ?", strings.Repeat("?, ", len(sources)-1)+"?"), args[6:8+len(sources)]...)
		err := cursor.Scan(&total)
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

func (db *SQLiteDatabase) SimilaritySearch(ctx context.Context, sourceID string, query []float32, limit int) ([]SimilarityResult, error) {

	serialized, err := vec.SerializeFloat32(query)
	if err != nil {
		return nil, err
	}

	rows, err := db.conn.QueryContext(ctx, fmt.Sprintf(`
	SELECT pages_vec_%s.distance, pages.url, pages.title, vec_chunks.chunk FROM pages_vec_%s
	JOIN vec_chunks USING (id)
	JOIN pages ON pages.id = vec_chunks.page
	WHERE
		pages_vec_%s.embedding MATCH ? AND
		pages.status = ? AND
		k = ?
	ORDER BY pages_vec_%s.distance
	LIMIT ?;
	`, sourceID, sourceID, sourceID, sourceID), serialized, Finished, limit, limit)

	if err != nil {
		return nil, err
	}

	results := make([]SimilarityResult, 0)

	for rows.Next() {
		res := SimilarityResult{}
		err := rows.Scan(&res.Similarity, &res.URL, &res.Title, &res.Chunk)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}

	return results, err
}

var tmpl *template.Template = template.Must(template.New("hybrid-search").Parse(`
WITH {{ range $index, $value := .Sources -}}
	vec_subquery_{{ $value }} AS (
		SELECT
			vec_chunks.page AS page,
			row_number() OVER (ORDER BY distance) AS rank_number,
			vec_chunks.chunk AS chunk,
			distance
		FROM pages_vec_{{ $value }}
		JOIN vec_chunks USING (id)
		WHERE embedding MATCH ? AND k = ?
		-- Select only the most relevant chunk for each page
		GROUP BY vec_chunks.page
		HAVING MIN(distance)
		ORDER BY distance
), {{ end }}fts_subquery AS (
	SELECT
		pages_fts.rowid AS page,
		highlight(pages_fts, 1, ?, ?) AS title,
		snippet(pages_fts, 2, ?, ?, '…', 8) AS description,
		snippet(pages_fts, 3, ?, ?, '…', 24) AS content,
		rank
	FROM pages_fts
	JOIN pages ON pages.id = pages_fts.rowid
	WHERE
		pages.source IN (
			{{- range $index, $value := .Sources -}}
				{{- if gt $index 0 }}, {{ end -}}
				?
			{{- end -}}
		)
		AND pages.status = ?
		AND pages_fts MATCH ?
	LIMIT ?
), fts_ordered AS (
	SELECT *, row_number() OVER (ORDER BY rank) AS rank_number
	FROM fts_subquery
)
SELECT
	pages.url,
	coalesce(fts_ordered.title, pages.title) AS title,
	coalesce(fts_ordered.description, pages.description) AS description,
	coalesce(fts_ordered.content
		{{- range $index, $value := .Sources -}}
			, vec_subquery_{{ $value }}.chunk
		{{- end -}}
	) AS content,
	{{ if eq (len .Sources) 1 -}}
		vec_subquery_{{ index .Sources 0 }}.distance AS vec_distance,
	{{ else -}}
		coalesce(
			{{- range $index, $value := .Sources -}}
				{{- if gt $index 0 }}, {{ end -}}
				vec_subquery_{{ $value }}.distance
			{{- end -}}
		) AS vec_distance,
	{{ end -}}

	{{ if eq (len .Sources) 1 -}}
		vec_subquery_{{ index .Sources 0 }}.rank_number AS vec_rank,
	{{- else -}}
		coalesce(
			{{- range $index, $value := .Sources -}}
				{{- if gt $index 0 }}, {{ end -}}
				vec_subquery_{{ $value }}.rank_number
			{{- end -}}
		) AS vec_rank,
	{{ end -}}

	fts_ordered.rank_number AS fts_rank,
	(
		{{ range $index, $value := .Sources -}}
			coalesce(1.0 / (60 + vec_subquery_{{ $value }}.rank_number) * 0.5, 0.0) +
		{{ end -}}
	coalesce(1.0 / (60 + fts_ordered.rank_number), 0.0)
	) AS combined_rank
FROM fts_ordered
{{ range $index, $value := .Sources -}}
	FULL OUTER JOIN vec_subquery_{{ $value }} USING (page)
{{ end -}}
JOIN pages ON pages.id = coalesce(
	fts_ordered.page {{- range $index, $value := .Sources -}}
		, vec_subquery_{{ $value }}.page
	{{- end }}
)
ORDER BY combined_rank DESC;
`))

func (db *SQLiteDatabase) HybridSearch(ctx context.Context, sources []string, queryString string, embeddedQueries map[string][]float32, limit int) ([]HybridResult, error) {

	// Convert the query vectors to a blob format that `sqlite-vec` will accept
	serializedQueries := make(map[string][]byte)

	for sourceID, query := range embeddedQueries {
		serialized, err := vec.SerializeFloat32(query)
		if err != nil {
			return nil, err
		}
		serializedQueries[sourceID] = serialized
	}

	type TemplateData struct {
		Sources []string
	}

	var query bytes.Buffer
	err := tmpl.Execute(&query, TemplateData{Sources: sources})
	if err != nil {
		return nil, fmt.Errorf("error formatting query: %v", err)
	}

	args := []any{}

	// Vector query args
	for _, src := range sources {
		args = append(args, serializedQueries[src], limit)
	}

	// FTS query args
	start := uuid.New().String()
	end := uuid.New().String()

	args = append(args, start, end, start, end, start, end)

	for _, src := range sources {
		args = append(args, src)
	}

	args = append(args, Finished, queryString, limit)

	rows, err := db.conn.QueryContext(ctx, query.String(), args...)

	if err != nil {
		return nil, err
	}

	results := make([]HybridResult, 0)

	for rows.Next() {
		res := HybridResult{}
		var title, description, content string
		err := rows.Scan(&res.URL, &title, &description, &content, &res.VecDistance, &res.VecRank, &res.FTSRank, &res.HybridRank)
		if err != nil {
			return nil, err
		}
		res.Title = processResult(title, start, end)
		res.Description = processResult(description, start, end)
		res.Content = processResult(content, start, end)
		results = append(results, res)
	}

	return results, err
}

func (db *SQLiteDatabase) AddToQueue(ctx context.Context, source string, referrer string, urls []string, depth int32, isRefresh bool) error {

	page, err := db.GetDocument(ctx, source, referrer)
	if err != nil {
		return fmt.Errorf("error looking up referring page: %v", err)
	}

	tx, err := db.conn.BeginTx(ctx, nil)

	if err != nil {
		return err
	}

	for _, url := range urls {
		// Insert a crawl queue entry (or use an existing one) and get its ID
		// The seemingly-useless ON CONFLICT clause makes sure the ID is returned, even if the row already exists
		var id int64
		err := tx.QueryRowContext(ctx, "INSERT INTO crawl_queue (source, url, depth, isRefresh) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET url = url RETURNING id;", source, url, depth, isRefresh).Scan(&id)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return rbErr
			}
			return err
		}

		// If there was a referring page, record it
		if page == nil {
			continue
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO crawl_queue_referrers (queueItem, referrer) VALUES (?, ?) ON CONFLICT DO NOTHING;", id, page.ID)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				return rbErr
			}
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (db *SQLiteDatabase) AddToEmbedQueue(ctx context.Context, pageID int64, chunks []string) error {

	tx, err := db.conn.BeginTx(ctx, nil)

	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM embed_queue WHERE page = ?;", pageID)
	if err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil {
			return rbErr
		}
		return err
	}

	i := 0
	for _, chunk := range chunks {
		_, err := tx.ExecContext(ctx, "INSERT INTO embed_queue (page, chunkIndex, chunk) VALUES (?, ?, ?);", pageID, i, chunk)
		if err != nil {
			rbErr := tx.Rollback()
			if rbErr != nil {
				return rbErr
			}
			return err
		}

		i++
	}

	err = tx.Commit()
	return err
}

func (db *SQLiteDatabase) PopQueue(ctx context.Context, source string) (*QueueItem, error) {
	// Find the first item in the queue and update it in one step. If the row isn't returned, another process must have updated it at the same time.
	row := db.conn.QueryRowContext(ctx, `
	  UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE rowid = (
	    SELECT rowid FROM crawl_queue WHERE status = ? AND source = ? ORDER BY addedAt LIMIT 1
	  ) RETURNING id, source, url, status, depth, isRefresh, addedAt, updatedAt;
	`, Processing, Pending, source)

	item := &QueueItem{}
	err := row.Scan(&item.ID, &item.Source, &item.URL, &item.Status, &item.Depth, &item.IsRefresh, &item.AddedAt, &item.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	var referrers []int64
	rows, err := db.conn.QueryContext(ctx, "SELECT referrer FROM crawl_queue_referrers WHERE queueItem = ?;", item.ID)

	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var r int64
		err := rows.Scan(&r)
		if err != nil {
			return nil, err
		}
		referrers = append(referrers, r)
	}

	item.Referrers = referrers

	return item, nil
}

func (db *SQLiteDatabase) PopEmbedQueue(ctx context.Context, limit int, source string) ([]EmbedQueueItem, error) {
	// Find the first item in the queue and update it in one step. If the row isn't returned, another process must have updated it at the same time.
	rows, err := db.conn.QueryContext(ctx, `
	  UPDATE embed_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE id IN (
	    SELECT embed_queue.id FROM embed_queue JOIN pages ON embed_queue.page = pages.id WHERE embed_queue.status = ? AND pages.source = ? ORDER BY embed_queue.addedAt LIMIT ?
	  ) RETURNING id, status, page, chunkIndex, chunk;
	`, Processing, Pending, source, limit)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	chunks := make([]EmbedQueueItem, 0, limit)

	for rows.Next() {

		item := EmbedQueueItem{}
		err := rows.Scan(&item.ID, &item.Status, &item.PageID, &item.ChunkIndex, &item.Content)

		if err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}

		chunks = append(chunks, item)
	}

	return chunks, nil
}

func (db *SQLiteDatabase) AddEmbedding(ctx context.Context, pageID int64, sourceID string, chunkIndex int, chunk string, vector []float32) error {
	serialized, err := vec.SerializeFloat32(vector)
	if err != nil {
		return err
	}

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return err
	}

	id := int64(-1)
	err = tx.QueryRowContext(ctx, "INSERT INTO vec_chunks (page, chunkIndex, chunk) VALUES (?, ?, ?) RETURNING id;", pageID, chunkIndex, chunk).Scan(&id)
	if err != nil {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return err
	}

	_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO pages_vec_%s (id, embedding) VALUES (?, ?);", sourceID), id, serialized)
	if err != nil {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return err
	}

	err = tx.Commit()

	return err
}

func (db *SQLiteDatabase) UpdateQueueEntry(ctx context.Context, id int64, status QueueItemStatus) error {
	if status == Finished {
		// If the item is finished, it can be immediately deleted
		_, err := db.conn.ExecContext(ctx, "DELETE FROM crawl_queue WHERE id = ?;", id)
		return err
	} else {
		_, err := db.conn.ExecContext(ctx, "UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE id = ?", status, id)
		return err
	}
}

func (db *SQLiteDatabase) UpdateEmbedQueueEntry(ctx context.Context, id int64, status QueueItemStatus) error {
	if status == Finished {
		// If the item is finished, it can be immediately deleted
		_, err := db.conn.ExecContext(ctx, "DELETE FROM embed_queue WHERE id = ?;", id)
		return err
	} else {
		_, err := db.conn.ExecContext(ctx, "UPDATE embed_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE id = ?;", status, id)
		return err
	}
}

func (db *SQLiteDatabase) Cleanup(ctx context.Context) error {
	_, err := db.conn.ExecContext(ctx, `
		-- Clear all Finished queue items (this is a sanity check; they should be immediately deleted from UpdateQueueEntry)
		DELETE FROM crawl_queue WHERE status = ?;

		-- Mark items that have been Processing for over a minute as Pending again
		UPDATE crawl_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE status = ? AND unixepoch() - unixepoch(updatedAt) > 60;

		-- Do the same for the embedding queue
		DELETE FROM embed_queue WHERE status = ?;
		UPDATE embed_queue SET status = ?, updatedAt = CURRENT_TIMESTAMP WHERE status IN (?, ?) AND unixepoch() - unixepoch(updatedAt) > 60;

		-- Remove embeddings which aren't linked to a page
		-- This should never happen because of the foreign key, but it seems to occur on rare occasion
		DELETE FROM embed_queue WHERE page NOT IN (SELECT id FROM pages);
		`, Finished, Pending, Processing, Finished, Pending, Error, Processing)

	return err
}

func (db *SQLiteDatabase) StartEmbeddings(ctx context.Context, source string, chunkSize int, chunkOverlap int) error {
	// If a page has been indexed but has no embeddings, make sure an embedding job has been queued
	rows, err := db.conn.QueryContext(ctx, "SELECT id, content FROM pages WHERE source = ? AND status = ? AND id NOT IN (SELECT page FROM vec_chunks) AND id NOT IN (SELECT page FROM embed_queue);", source, Finished)
	if err != nil {
		return fmt.Errorf("error finding pages without embeddings: %v", err)
	}
	for rows.Next() {
		var id int64
		var content string
		err := rows.Scan(&id, &content)
		if err != nil {
			return err
		}

		chunks, err := embedding.ChunkText(content, chunkSize, chunkOverlap)

		if err != nil {
			return fmt.Errorf("error chunking page: %v", err)
		}

		// Filter out empty chunks
		filtered := make([]string, 0, len(chunks))
		for _, chunk := range chunks {
			if len(strings.TrimSpace(chunk)) != 0 {
				filtered = append(filtered, chunk)
			}
		}

		err = db.AddToEmbedQueue(ctx, id, filtered)
		if err != nil {
			return fmt.Errorf("error adding page to embedding queue: %v", err)
		}
	}
	return nil
}

func (db *SQLiteDatabase) QueuePagesOlderThan(ctx context.Context, source string, daysAgo int32) error {
	rows, err := db.conn.QueryContext(ctx, "SELECT source, url, crawledAt, depth, status FROM pages WHERE url NOT IN (SELECT url FROM crawl_queue) AND source = ? AND unixepoch() - unixepoch(crawledAt) > ?", source, daysAgo*86400)

	if err != nil {
		return err
	}

	for rows.Next() {

		row := &Page{}

		err := rows.Scan(&row.Source, &row.URL, &row.CrawledAt, &row.Depth, &row.Status)

		if err != nil {
			return err
		}

		// The referrer is blank because the `pages` table entry already has all of its referrers recorded
		err = db.AddToQueue(ctx, source, "", []string{row.URL}, row.Depth, true)

		if err != nil {
			return err
		}
	}

	return nil
}

func (db *SQLiteDatabase) GetCanonical(ctx context.Context, source string, url string) (*Canonical, error) {
	cursor := db.conn.QueryRowContext(ctx, "SELECT id, url, canonical, crawledAt FROM canonicals WHERE source = ? AND url = ?", source, url)

	canonical := &Canonical{}
	err := cursor.Scan(&canonical.ID, &canonical.Original, &canonical.Canonical, &canonical.CrawledAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return canonical, nil
}

func (db *SQLiteDatabase) SetCanonical(ctx context.Context, source string, url string, canonical string) error {
	_, err := db.conn.ExecContext(ctx, "REPLACE INTO canonicals (source, url, canonical) VALUES (?, ?, ?)", source, url, canonical)
	return err
}

func SQLiteFromFile(fileName string) (*SQLiteDatabase, error) {
	vec.Auto() // Load the `sqlite-vec` extension
	conn, err := sql.Open("sqlite3", fileName)

	if err != nil {
		return nil, err
	}

	return SQLite(conn)
}

func SQLite(conn *sql.DB) (*SQLiteDatabase, error) {
	return &SQLiteDatabase{conn}, nil
}
