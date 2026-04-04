package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/search"
)

// NewSearchCommand returns the "moca search" command group with all subcommands.
func NewSearchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search index management",
		Long:  "Rebuild, query, and monitor Meilisearch indexes.",
	}

	cmd.AddCommand(
		newSearchRebuildCmd(),
		newSearchStatusCmd(),
		newSearchQueryCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// search rebuild
// ---------------------------------------------------------------------------

func newSearchRebuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild search index for a site/doctype",
		Long: `Rebuild Meilisearch indexes from database. Re-indexes all documents
for the specified site and doctype(s).`,
		RunE: runSearchRebuild,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site name")
	f.Bool("all-sites", false, "Rebuild for all active sites")
	f.String("doctype", "", "Rebuild for a specific DocType only")
	f.Int("batch-size", 1000, "Batch size for indexing")

	return cmd
}

func runSearchRebuild(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Determine target sites.
	allSites, _ := cmd.Flags().GetBool("all-sites")
	var sites []string
	if allSites {
		sites, err = listActiveSites(cmd.Context(), svc)
		if err != nil {
			return output.NewCLIError("Failed to list active sites").WithErr(err)
		}
		if len(sites) == 0 {
			return output.NewCLIError("No active sites found").
				WithFix("Create a site with 'moca site create'.")
		}
	} else {
		siteName, siteErr := resolveSiteName(cmd, ctx)
		if siteErr != nil {
			return siteErr
		}
		sites = []string{siteName}
	}

	// Create search client and indexer.
	searchClient, err := search.NewClient(ctx.Project.Infrastructure.Search)
	if err != nil {
		return output.NewCLIError("Cannot connect to Meilisearch").
			WithErr(err).
			WithFix("Ensure Meilisearch is running and configured in moca.yaml.")
	}
	defer searchClient.Close()

	indexer := search.NewIndexer(searchClient)

	doctypeFilter, _ := cmd.Flags().GetString("doctype")
	batchSize, _ := cmd.Flags().GetInt("batch-size")
	if batchSize <= 0 {
		batchSize = 1000
	}

	type rebuildResult struct {
		Site    string `json:"site"`
		DocType string `json:"doctype"`
		Error   string `json:"error,omitempty"`
		Indexed int    `json:"indexed"`
	}
	var results []rebuildResult

	for _, site := range sites {
		pool, poolErr := svc.DB.ForSite(cmd.Context(), site)
		if poolErr != nil {
			return output.NewCLIError("Cannot connect to site database").
				WithErr(poolErr).
				WithContext("site: " + site)
		}

		// Get searchable doctypes.
		query := `SELECT name FROM tab_doctype ORDER BY name`
		rows, queryErr := pool.Query(cmd.Context(), query)
		if queryErr != nil {
			return output.NewCLIError("Failed to query doctypes").
				WithErr(queryErr).
				WithContext("site: " + site)
		}

		var doctypes []string
		for rows.Next() {
			var name string
			if scanErr := rows.Scan(&name); scanErr != nil {
				rows.Close()
				return output.NewCLIError("Failed to scan doctype").WithErr(scanErr)
			}
			if doctypeFilter != "" && !strings.EqualFold(name, doctypeFilter) {
				continue
			}
			doctypes = append(doctypes, name)
		}
		rows.Close()
		if rows.Err() != nil {
			return output.NewCLIError("Failed to iterate doctypes").WithErr(rows.Err())
		}

		if doctypeFilter != "" && len(doctypes) == 0 {
			return output.NewCLIError(fmt.Sprintf("DocType %q not found in site %q", doctypeFilter, site)).
				WithFix("Check the doctype name and try again.")
		}

		for _, dt := range doctypes {
			mt, mtErr := svc.Registry.Get(cmd.Context(), site, dt)
			if mtErr != nil {
				w.Debugf("skipping %s: %v", dt, mtErr)
				continue
			}

			// Check if doctype has searchable fields.
			hasSearchable := false
			for _, field := range mt.Fields {
				if field.Searchable {
					hasSearchable = true
					break
				}
			}
			if !hasSearchable {
				w.Debugf("skipping %s: no searchable fields", dt)
				continue
			}

			w.Print("Rebuilding index for %s.%s...", w.Color().Bold(site), w.Color().Bold(dt))

			// Count total rows for progress bar.
			tableName := "tab_" + strings.ToLower(dt)
			var totalRows int
			countErr := pool.QueryRow(cmd.Context(), fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&totalRows)
			if countErr != nil {
				w.Debugf("cannot count %s: %v", tableName, countErr)
				results = append(results, rebuildResult{Site: site, DocType: dt, Error: countErr.Error()})
				continue
			}

			if totalRows == 0 {
				// Ensure index exists even if empty.
				if ensErr := indexer.EnsureIndex(cmd.Context(), site, mt); ensErr != nil {
					results = append(results, rebuildResult{Site: site, DocType: dt, Error: ensErr.Error()})
					continue
				}
				results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: 0})
				w.Print("  %s No documents to index", w.Color().Muted("-"))
				continue
			}

			pb := w.NewProgressBar(totalRows)
			indexed := 0
			offset := 0

			for offset < totalRows {
				dataRows, dataErr := pool.Query(cmd.Context(),
					fmt.Sprintf("SELECT * FROM %s ORDER BY name LIMIT $1 OFFSET $2", tableName),
					batchSize, offset,
				)
				if dataErr != nil {
					results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: indexed, Error: dataErr.Error()})
					break
				}

				fieldDescs := dataRows.FieldDescriptions()
				var batch []map[string]any
				for dataRows.Next() {
					values, scanErr := dataRows.Values()
					if scanErr != nil {
						dataRows.Close()
						results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: indexed, Error: scanErr.Error()})
						break
					}
					doc := make(map[string]any, len(fieldDescs))
					for i, fd := range fieldDescs {
						doc[string(fd.Name)] = values[i]
					}
					batch = append(batch, doc)
				}
				dataRows.Close()
				if dataRows.Err() != nil {
					results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: indexed, Error: dataRows.Err().Error()})
					break
				}

				if len(batch) > 0 {
					if idxErr := indexer.IndexDocuments(cmd.Context(), site, mt, batch); idxErr != nil {
						results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: indexed, Error: idxErr.Error()})
						break
					}
					indexed += len(batch)
				}

				offset += batchSize
				pb.Update(indexed)
			}
			pb.Finish()

			if indexed == totalRows || (len(results) > 0 && results[len(results)-1].DocType == dt && results[len(results)-1].Error != "") {
				// Already added with error
			} else {
				results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: indexed})
			}

			// Add result if not already added via error path
			found := false
			for _, r := range results {
				if r.Site == site && r.DocType == dt {
					found = true
					break
				}
			}
			if !found {
				results = append(results, rebuildResult{Site: site, DocType: dt, Indexed: indexed})
			}
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(results)
	}

	w.Print("")
	totalIndexed := 0
	for _, r := range results {
		totalIndexed += r.Indexed
		if r.Error != "" {
			w.Print("  %s %s.%s: %s", w.Color().Error("✗"), r.Site, r.DocType, r.Error)
		} else {
			w.Print("  %s %s.%s: %d document(s)", w.Color().Success("✓"), r.Site, r.DocType, r.Indexed)
		}
	}
	w.Print("")
	w.PrintSuccess(fmt.Sprintf("Indexed %d document(s) across %d index(es)", totalIndexed, len(results)))

	return nil
}

// ---------------------------------------------------------------------------
// search status
// ---------------------------------------------------------------------------

func newSearchStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show search index status",
		Long:  "Show Meilisearch index status including document counts and sizes.",
		RunE:  runSearchStatus,
	}

	f := cmd.Flags()
	f.String("site", "", "Filter by site")

	return cmd
}

func runSearchStatus(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	searchClient, err := search.NewClient(ctx.Project.Infrastructure.Search)
	if err != nil {
		return output.NewCLIError("Cannot connect to Meilisearch").
			WithErr(err).
			WithFix("Ensure Meilisearch is running and configured in moca.yaml.")
	}
	defer searchClient.Close()

	// Determine prefix filter based on --site flag.
	prefix := ""
	if site, _ := cmd.Flags().GetString("site"); site != "" {
		prefix = site + "_"
	}

	s := w.NewSpinner("Fetching index status...")
	s.Start()

	indexes, err := searchClient.ListIndexes(cmd.Context(), prefix)
	if err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to list indexes").WithErr(err)
	}

	type indexStatus struct {
		UID       string `json:"uid"`
		Site      string `json:"site"`
		DocType   string `json:"doctype"`
		UpdatedAt string `json:"updated_at"`
		Documents int64  `json:"documents"`
		Size      int64  `json:"size_bytes"`
		Indexing  bool   `json:"is_indexing"`
	}
	var statuses []indexStatus

	for _, idx := range indexes {
		stats, statsErr := searchClient.GetIndexStats(cmd.Context(), idx.UID)

		site, doctype := parseIndexUID(idx.UID)

		entry := indexStatus{
			UID:       idx.UID,
			Site:      site,
			DocType:   doctype,
			UpdatedAt: idx.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		if statsErr == nil {
			entry.Documents = stats.NumberOfDocuments
			entry.Size = stats.RawDocumentDbSize
			entry.Indexing = stats.IsIndexing
		}
		statuses = append(statuses, entry)
	}

	s.Stop("Index status retrieved")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(statuses)
	}

	if len(statuses) == 0 {
		w.PrintInfo("No search indexes found.")
		return nil
	}

	headers := []string{"SITE", "DOCTYPE", "DOCUMENTS", "SIZE", "LAST UPDATED"}
	var rows [][]string
	for _, st := range statuses {
		status := ""
		if st.Indexing {
			status = " (indexing)"
		}
		rows = append(rows, []string{
			st.Site,
			st.DocType,
			strconv.FormatInt(st.Documents, 10) + status,
			formatBytes(st.Size),
			st.UpdatedAt,
		})
	}

	return w.PrintTable(headers, rows)
}

// parseIndexUID extracts site and doctype from a Meilisearch index UID.
// Format: "{site}_{doctype}" (e.g., "acme.localhost_SalesOrder").
func parseIndexUID(uid string) (site, doctype string) {
	idx := strings.Index(uid, "_")
	if idx < 0 {
		return uid, ""
	}
	return uid[:idx], uid[idx+1:]
}

// ---------------------------------------------------------------------------
// search query
// ---------------------------------------------------------------------------

func newSearchQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query QUERY",
		Short: "Query search index from CLI",
		Long:  "Search from the command line using Meilisearch.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSearchQuery,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site (required)")
	f.String("doctype", "", "Search within a specific DocType (required)")
	f.Int("limit", 10, "Max results")

	return cmd
}

func runSearchQuery(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	query := args[0]

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	siteName, err := resolveSiteName(cmd, ctx)
	if err != nil {
		return err
	}

	doctype, _ := cmd.Flags().GetString("doctype")
	if doctype == "" {
		return output.NewCLIError("--doctype is required").
			WithFix("Pass --doctype <DocType> to specify which index to search.")
	}

	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 10
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Load MetaType for the doctype.
	mt, mtErr := svc.Registry.Get(cmd.Context(), siteName, doctype)
	if mtErr != nil {
		return output.NewCLIError(fmt.Sprintf("Cannot load MetaType %q", doctype)).
			WithErr(mtErr).
			WithContext("site: " + siteName)
	}

	searchClient, err := search.NewClient(ctx.Project.Infrastructure.Search)
	if err != nil {
		return output.NewCLIError("Cannot connect to Meilisearch").
			WithErr(err).
			WithFix("Ensure Meilisearch is running and configured in moca.yaml.")
	}
	defer searchClient.Close()

	qs := search.NewQueryService(searchClient)

	results, total, searchErr := qs.Search(cmd.Context(), siteName, mt, query, nil, 1, limit)
	if searchErr != nil {
		return output.NewCLIError("Search failed").
			WithErr(searchErr).
			WithContext(fmt.Sprintf("query=%q doctype=%s site=%s", query, doctype, siteName))
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"query":   query,
			"site":    siteName,
			"doctype": doctype,
			"total":   total,
			"results": results,
		})
	}

	if len(results) == 0 {
		w.PrintInfo(fmt.Sprintf("No results for %q in %s.%s", query, siteName, doctype))
		return nil
	}

	headers := []string{"SCORE", "DOCTYPE", "NAME"}
	var rows [][]string
	for _, r := range results {
		rows = append(rows, []string{
			fmt.Sprintf("%.2f", r.Score),
			r.DocType,
			r.Name,
		})
	}

	if err := w.PrintTable(headers, rows); err != nil {
		return err
	}

	w.Print("")
	w.Print("%s", w.Color().Muted(fmt.Sprintf("Showing %d of %d result(s)", len(results), total)))

	return nil
}
