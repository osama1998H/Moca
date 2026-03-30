// Package storage implements file storage for MOCA using S3-compatible backends.
//
// Attachments and assets are stored in S3 or MinIO. Access control is enforced
// at the application layer based on document permissions. In development mode,
// the local filesystem is used as the storage backend.
//
// Key components:
//   - S3: S3/MinIO adapter with upload, download, delete, and presigned URLs
//   - Manager: file lifecycle management with per-site path namespacing
//   - Thumbnail: on-demand image thumbnail generation
package storage
