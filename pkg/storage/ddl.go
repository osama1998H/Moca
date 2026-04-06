package storage

// SQL query constants for tab_file operations.
const (
	insertFileSQL = `INSERT INTO tab_file
		("name", "file_name", "file_url", "file_size", "content_type",
		 "attached_to_doctype", "attached_to_name", "is_private", "owner")
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	selectFileByNameSQL = `SELECT
		"name", "file_name", "file_url", "file_size", "content_type",
		"attached_to_doctype", "attached_to_name", "is_private", "owner", "creation"
		FROM tab_file WHERE "name" = $1`

	deleteFileByNameSQL = `DELETE FROM tab_file WHERE "name" = $1`

	selectFilesByRefSQL = `SELECT
		"name", "file_name", "file_url", "file_size", "content_type",
		"attached_to_doctype", "attached_to_name", "is_private", "owner", "creation"
		FROM tab_file
		WHERE "attached_to_doctype" = $1 AND "attached_to_name" = $2
		ORDER BY "creation" DESC`

)
