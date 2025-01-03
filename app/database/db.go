package database

import "context"

type Database interface {
	// Create necessary tables
	Setup(ctx context.Context) error
	SetupVectorTables(ctx context.Context, sourceID string, dimension int) error
	DropVectorTables(ctx context.Context, sourceID string) error

	// Clears out unused data and marks queue items that have been Processing for a while as Pending
	Cleanup(ctx context.Context) error

	// Add a page to the search index.
	AddDocument(ctx context.Context, source string, depth int32, referrers []int64, url string, status QueueItemStatus, title string, description string, content string, errorInfo string) (int64, error)
	// Returns whether the given URL (or the URL's canonical) is indexed
	HasDocument(ctx context.Context, source string, url string) (*bool, error)
	// Fetch the document by URL (or the URL's canonical)
	GetDocument(ctx context.Context, source string, url string) (*Page, error)
	GetDocumentByID(ctx context.Context, id int64) (*Page, error)
	// Delete a document by its URL and remove all canonicals pointing to it
	RemoveDocument(ctx context.Context, source string, url string) error

	// Records all the pages that a page links to for future reference.
	AddReferrer(ctx context.Context, source int64, dest int64) error
	// Removes an existing referrer entry, given a source and destination page.
	RemoveReferrer(ctx context.Context, source int64, dest int64) error
	// Removes all referrer entries for the pages that this page refers to. Used when refreshing pages to remove old entries before calling AddReferrer with new entries.
	RemoveAllReferences(ctx context.Context, source int64) error
	// Get a list of all the page IDs that this page links to.
	GetReferences(ctx context.Context, pageID int64) ([]int64, error)
	// Get a list of all the page IDs that link to this page.
	GetReferrers(ctx context.Context, pageID int64) ([]int64, error)
	// Lists pages with no referrers. These pages should usually be deleted, unless they're the source's base URL.
	ListOrphanPages(ctx context.Context) ([]int64, error)

	// Run a fulltext search with the given query
	Search(ctx context.Context, sources []string, query string, page uint32, pageSize uint32) ([]FTSResult, *uint32, error)

	// Add an item to the crawl queue
	AddToQueue(ctx context.Context, source string, referrer string, urls []string, depth int32, isRefresh bool) error
	// Update the status of the item in the queue by its ID
	UpdateQueueEntry(ctx context.Context, id int64, status QueueItemStatus) error
	// Sets the first item in the queue to `Processing` and returns it. If both the item and `error` is nil, the queue is empty OR another worker already claimed the row.
	PopQueue(ctx context.Context, source string) (*QueueItem, error)
	// Add pages older than `daysAgo` to the queue to be recrawled.
	QueuePagesOlderThan(ctx context.Context, source string, daysAgo int32) error

	GetCanonical(ctx context.Context, source string, url string) (*Canonical, error)
	SetCanonical(ctx context.Context, source string, url string, canonical string) error

	// Embedding/similarity search-related methods:

	// Add text chunks to the embedding queue
	AddToEmbedQueue(ctx context.Context, id int64, chunks []string) error
	PopEmbedQueue(ctx context.Context, limit int, source string) ([]EmbedQueueItem, error)
	UpdateEmbedQueueEntry(ctx context.Context, id int64, status QueueItemStatus) error
	AddEmbedding(ctx context.Context, pageID int64, sourceID string, chunkIndex int, chunk string, vector []float32) error

	// Adds pages with no embeddings to the embed queue
	StartEmbeddings(ctx context.Context, source string, chunkSize int, chunkOverlap int) error

	SimilaritySearch(ctx context.Context, sourceID string, query []float32, limit int) ([]SimilarityResult, error)
	HybridSearch(ctx context.Context, sources []string, queryString string, embeddedQueries map[string][]float32, limit int) ([]HybridResult, error)

	// Use vocabulary from the search corpus to set up spelling correction
	CreateSpellfixIndex(ctx context.Context) error
	// Remove any created indexes for spell checking to free up disk space
	DropSpellfixIndex(ctx context.Context) error

	// Attempt to fix spelling errors using a dictionary
	Spellfix(ctx context.Context, query string) (string, error)
}

type Page struct {
	ID          int64           `json:"id"`
	Source      string          `json:"source"`
	URL         string          `json:"url"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Depth       int32           `json:"depth"`
	CrawledAt   string          `json:"crawledAt"`
	Status      QueueItemStatus `json:"status"`
	ErrorInfo   string          `json:"error"`
}

type FTSResult struct {
	URL         string  `json:"url"`
	Title       []Match `json:"title"`
	Description []Match `json:"description"`
	Content     []Match `json:"content"`
	Rank        float64 `json:"rank"`
}

type SimilarityResult struct {
	URL        string  `json:"url"`
	Title      string  `json:"title"`
	Chunk      string  `json:"chunk"`
	Similarity float32 `json:"similarity"`
}

type HybridResult struct {
	URL         string   `json:"url"`
	Title       []Match  `json:"title"`
	Description []Match  `json:"description"`
	Content     []Match  `json:"content"`
	FTSRank     *int     `json:"ftsRank"`
	VecRank     *int     `json:"vecRank"`
	VecDistance *float64 `json:"vecDistance"`
	HybridRank  float64  `json:"rank"`
}

type Match struct {
	Highlighted bool   `json:"highlighted"`
	Content     string `json:"content"`
}

type QueueItemStatus int8

const (
	Pending QueueItemStatus = iota
	Processing
	Finished
	Error

	// Used in "pages" tables to record that a page _has_ been indexed, but doesn't have any content.
	// For example, a sitemap or RSS feed would be added as Unindexable with an empty title and description.
	Unindexable
)

type QueueItem struct {
	ID        int64
	Source    string
	URL       string
	AddedAt   string
	UpdatedAt string
	Depth     int32
	IsRefresh bool
	Referrers []int64
	Status    QueueItemStatus
}

type Canonical struct {
	ID        int64
	Original  string
	Canonical string
	CrawledAt string
}

type EmbedQueueItem struct {
	ID         int64
	Status     QueueItemStatus
	PageID     int64
	ChunkIndex int
	Content    string
}
