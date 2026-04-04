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

var ErrUnavailable = errors.New("search unavailable")

const meiliTaskPollInterval = 50 * time.Millisecond

type rawService interface {
	Index(uid string) rawIndex
	CreateIndexWithContext(ctx context.Context, cfg *meilisearch.IndexConfig) (*meilisearch.TaskInfo, error)
	DeleteIndexWithContext(ctx context.Context, uid string) (*meilisearch.TaskInfo, error)
	GetIndexWithContext(ctx context.Context, uid string) (*meilisearch.IndexResult, error)
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
