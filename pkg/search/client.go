package search

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/meilisearch/meilisearch-go"

	"github.com/osama1998H/moca/internal/config"
)

// IndexInfo is a simplified representation of a Meilisearch index.
type IndexInfo struct {
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	UID        string    `json:"uid"`
	PrimaryKey string    `json:"primary_key"`
}

// IndexStats holds statistics for a single Meilisearch index.
type IndexStats struct {
	NumberOfDocuments int64 `json:"number_of_documents"`
	IsIndexing        bool  `json:"is_indexing"`
	RawDocumentDbSize int64 `json:"raw_document_db_size"`
}

var ErrUnavailable = errors.New("search unavailable")

const meiliTaskPollInterval = 50 * time.Millisecond

type rawService interface {
	Index(uid string) rawIndex
	CreateIndexWithContext(ctx context.Context, cfg *meilisearch.IndexConfig) (*meilisearch.TaskInfo, error)
	DeleteIndexWithContext(ctx context.Context, uid string) (*meilisearch.TaskInfo, error)
	GetIndexWithContext(ctx context.Context, uid string) (*meilisearch.IndexResult, error)
	ListIndexesWithContext(ctx context.Context, param *meilisearch.IndexesQuery) (*meilisearch.IndexesResults, error)
	WaitForTaskWithContext(ctx context.Context, taskUID int64, interval time.Duration) (*meilisearch.Task, error)
	Close()
}

type rawIndex interface {
	AddDocumentsInBatchesWithContext(ctx context.Context, documentsPtr interface{}, batchSize int, opts *meilisearch.DocumentOptions) ([]meilisearch.TaskInfo, error)
	DeleteDocumentWithContext(ctx context.Context, identifier string, opts *meilisearch.DocumentOptions) (*meilisearch.TaskInfo, error)
	UpdateDisplayedAttributesWithContext(ctx context.Context, request *[]string) (*meilisearch.TaskInfo, error)
	UpdateFilterableAttributesWithContext(ctx context.Context, request *[]interface{}) (*meilisearch.TaskInfo, error)
	UpdateSearchableAttributesWithContext(ctx context.Context, request *[]string) (*meilisearch.TaskInfo, error)
	SearchWithContext(ctx context.Context, query string, request *meilisearch.SearchRequest) (*meilisearch.SearchResponse, error)
	GetStatsWithContext(ctx context.Context) (*meilisearch.StatsIndex, error)
	WaitForTaskWithContext(ctx context.Context, taskUID int64, interval time.Duration) (*meilisearch.Task, error)
}

type meiliServiceAdapter struct {
	svc meilisearch.ServiceManager
}

func (m *meiliServiceAdapter) Index(uid string) rawIndex {
	return &meiliIndexAdapter{idx: m.svc.Index(uid)}
}

func (m *meiliServiceAdapter) CreateIndexWithContext(ctx context.Context, cfg *meilisearch.IndexConfig) (*meilisearch.TaskInfo, error) {
	return m.svc.CreateIndexWithContext(ctx, cfg)
}

func (m *meiliServiceAdapter) DeleteIndexWithContext(ctx context.Context, uid string) (*meilisearch.TaskInfo, error) {
	return m.svc.DeleteIndexWithContext(ctx, uid)
}

func (m *meiliServiceAdapter) GetIndexWithContext(ctx context.Context, uid string) (*meilisearch.IndexResult, error) {
	return m.svc.GetIndexWithContext(ctx, uid)
}

func (m *meiliServiceAdapter) ListIndexesWithContext(ctx context.Context, param *meilisearch.IndexesQuery) (*meilisearch.IndexesResults, error) {
	return m.svc.ListIndexesWithContext(ctx, param)
}

func (m *meiliServiceAdapter) WaitForTaskWithContext(ctx context.Context, taskUID int64, interval time.Duration) (*meilisearch.Task, error) {
	return m.svc.TaskReader().WaitForTaskWithContext(ctx, taskUID, interval)
}

func (m *meiliServiceAdapter) Close() {
	m.svc.Close()
}

type meiliIndexAdapter struct {
	idx meilisearch.IndexManager
}

func (m *meiliIndexAdapter) AddDocumentsInBatchesWithContext(ctx context.Context, documentsPtr interface{}, batchSize int, opts *meilisearch.DocumentOptions) ([]meilisearch.TaskInfo, error) {
	return m.idx.AddDocumentsInBatchesWithContext(ctx, documentsPtr, batchSize, opts)
}

func (m *meiliIndexAdapter) DeleteDocumentWithContext(ctx context.Context, identifier string, opts *meilisearch.DocumentOptions) (*meilisearch.TaskInfo, error) {
	return m.idx.DeleteDocumentWithContext(ctx, identifier, opts)
}

func (m *meiliIndexAdapter) UpdateDisplayedAttributesWithContext(ctx context.Context, request *[]string) (*meilisearch.TaskInfo, error) {
	return m.idx.UpdateDisplayedAttributesWithContext(ctx, request)
}

func (m *meiliIndexAdapter) UpdateFilterableAttributesWithContext(ctx context.Context, request *[]interface{}) (*meilisearch.TaskInfo, error) {
	return m.idx.UpdateFilterableAttributesWithContext(ctx, request)
}

func (m *meiliIndexAdapter) UpdateSearchableAttributesWithContext(ctx context.Context, request *[]string) (*meilisearch.TaskInfo, error) {
	return m.idx.UpdateSearchableAttributesWithContext(ctx, request)
}

func (m *meiliIndexAdapter) SearchWithContext(ctx context.Context, query string, request *meilisearch.SearchRequest) (*meilisearch.SearchResponse, error) {
	return m.idx.SearchWithContext(ctx, query, request)
}

func (m *meiliIndexAdapter) GetStatsWithContext(ctx context.Context) (*meilisearch.StatsIndex, error) {
	return m.idx.GetStatsWithContext(ctx)
}

func (m *meiliIndexAdapter) WaitForTaskWithContext(ctx context.Context, taskUID int64, interval time.Duration) (*meilisearch.Task, error) {
	return m.idx.WaitForTaskWithContext(ctx, taskUID, interval)
}

// Client wraps the Meilisearch SDK so the rest of the package can depend on a
// narrow interface.
type Client struct {
	svc rawService
}

func NewClient(cfg config.SearchConfig) (*Client, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.Engine), "meilisearch") {
		return nil, ErrUnavailable
	}

	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, ErrUnavailable
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("parse search host: %w", err)
	}
	if cfg.Port > 0 && parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), fmt.Sprintf("%d", cfg.Port))
	}
	host = parsed.String()

	opts := make([]meilisearch.Option, 0, 1)
	if strings.TrimSpace(cfg.APIKey) != "" {
		opts = append(opts, meilisearch.WithAPIKey(cfg.APIKey))
	}

	svc, err := meilisearch.Connect(host, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect meilisearch: %w", err)
	}

	return &Client{svc: &meiliServiceAdapter{svc: svc}}, nil
}

func (c *Client) available() bool {
	return c != nil && c.svc != nil
}

func (c *Client) Close() {
	if c != nil && c.svc != nil {
		c.svc.Close()
	}
}

func (c *Client) waitForTask(ctx context.Context, taskUID int64) error {
	if !c.available() {
		return ErrUnavailable
	}
	task, err := c.svc.WaitForTaskWithContext(ctx, taskUID, meiliTaskPollInterval)
	if err != nil {
		return err
	}
	if task.Status == meilisearch.TaskStatusFailed {
		return fmt.Errorf("task %d failed: %s", task.TaskUID, task.Error.Message)
	}
	return nil
}

func (c *Client) waitForIndexTask(ctx context.Context, index rawIndex, taskUID int64) error {
	task, err := index.WaitForTaskWithContext(ctx, taskUID, meiliTaskPollInterval)
	if err != nil {
		return err
	}
	if task.Status == meilisearch.TaskStatusFailed {
		return fmt.Errorf("task %d failed: %s", task.TaskUID, task.Error.Message)
	}
	return nil
}

// ListIndexes returns all indexes whose UID starts with prefix.
// Pass an empty prefix to list all indexes.
func (c *Client) ListIndexes(ctx context.Context, prefix string) ([]IndexInfo, error) {
	if !c.available() {
		return nil, ErrUnavailable
	}

	var allResults []IndexInfo
	var offset int64
	const pageSize int64 = 100

	for {
		resp, err := c.svc.ListIndexesWithContext(ctx, &meilisearch.IndexesQuery{
			Limit:  pageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, fmt.Errorf("list indexes: %w", err)
		}

		for _, idx := range resp.Results {
			if prefix != "" && !strings.HasPrefix(idx.UID, prefix) {
				continue
			}
			allResults = append(allResults, IndexInfo{
				UID:        idx.UID,
				PrimaryKey: idx.PrimaryKey,
				CreatedAt:  idx.CreatedAt,
				UpdatedAt:  idx.UpdatedAt,
			})
		}

		offset += pageSize
		if offset >= resp.Total {
			break
		}
	}

	return allResults, nil
}

// GetIndexStats returns statistics for a specific index by UID.
func (c *Client) GetIndexStats(ctx context.Context, uid string) (*IndexStats, error) {
	if !c.available() {
		return nil, ErrUnavailable
	}

	index := c.svc.Index(uid)
	stats, err := index.GetStatsWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("get index stats %q: %w", uid, err)
	}

	return &IndexStats{
		NumberOfDocuments: stats.NumberOfDocuments,
		IsIndexing:        stats.IsIndexing,
		RawDocumentDbSize: stats.RawDocumentDbSize,
	}, nil
}

func isSearchNotFound(err error) bool {
	var meiliErr *meilisearch.Error
	if errors.As(err, &meiliErr) {
		return meiliErr.StatusCode == 404
	}
	return false
}

func isSearchConflict(err error) bool {
	var meiliErr *meilisearch.Error
	if errors.As(err, &meiliErr) {
		return meiliErr.StatusCode == 409
	}
	return false
}
