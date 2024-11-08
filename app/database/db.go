package database

type Database interface {
	// Create necessary tables
	Setup() error

	// Add a page to the search index.
	AddDocument(source string, depth int32, referrer string, url string, status QueueItemStatus, title string, description string, content string, errorInfo string) error
	// Returns whether the given URL (or the URL's canonical) is indexed
	HasDocument(source string, url string) (*bool, error)
	// Fetch the document by URL (or the URL's canonical)
	GetDocument(source string, url string) (*Page, error)
	// Delete a document by its URL and remove all canonicals pointing to it
	RemoveDocument(source string, url string) error

	// Run a fulltext search with the given query
	Search(sources []string, query string, page uint32, pageSize uint32) ([]Result, *uint32, error)

	// Add an item to the crawl queue
	AddToQueue(source string, referrer string, urls []string, depth int32, isRefresh bool) error
	// Update the status of the item in the queue denoted by the specified url
	UpdateQueueEntry(source string, url string, status QueueItemStatus) error
	// Sets the first item in the queue to `Processing` and returns it. If both the item and `error` is nil, the queue is empty OR another worker already claimed the row.
	PopQueue(source string) (*QueueItem, error)
	// Set the status of items that have been Processing for over 20 minutes to Pending and remove any Finished entries
	CleanQueue() error
	// Add pages older than `daysAgo` to the queue to be recrawled.
	QueuePagesOlderThan(source string, daysAgo int32) error

	GetCanonical(source string, url string) (*Canonical, error)
	SetCanonical(source string, url string, canonical string) error
}

type Page struct {
	Source      string          `json:"source"`
	Referrer    string          `json:"referrer"`
	URL         string          `json:"url"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Depth       int32           `json:"depth"`
	CrawledAt   string          `json:"crawledAt"`
	Status      QueueItemStatus `json:"status"`
	ErrorInfo   string          `json:"error"`
}

type Result struct {
	URL         string  `json:"url"`
	Title       []Match `json:"title"`
	Description []Match `json:"description"`
	Content     []Match `json:"content"`
	Rank        float64 `json:"rank"`
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
	Source    string
	URL       string
	AddedAt   string
	UpdatedAt string
	Depth     int32
	IsRefresh bool
	Referrer  string
	Status    QueueItemStatus
}

type Canonical struct {
	Original  string
	Canonical string
	CrawledAt string
}
