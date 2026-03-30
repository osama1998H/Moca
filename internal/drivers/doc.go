// Package drivers provides client factories for external infrastructure
// services (Redis, Meilisearch, S3) used by the Moca framework.
//
// Each driver type provides:
//   - A constructor that accepts the corresponding config struct and a logger
//   - A Ping method to verify connectivity
//   - A Close method to release resources
//
// Implemented in MS-02-T3: Redis client factory.
// Meilisearch and S3 drivers are scheduled for later milestones.
package drivers
