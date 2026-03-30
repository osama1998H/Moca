package config

// ResolveInheritance applies staging inheritance in-place on cfg.
// If cfg.Staging.Inherits == "production", each nil staging pointer field is
// populated with the corresponding value from cfg.Production, so the caller can
// treat cfg.Staging as a fully-resolved environment config.
//
// The function is a no-op when Staging.Inherits is empty or already resolved.
// Only "production" is a valid inheritance target for MS-01; invalid values are
// caught by Validate and silently ignored here.
func ResolveInheritance(cfg *ProjectConfig) {
	if cfg.Staging.Inherits != "production" {
		return
	}

	prod := &cfg.Production
	stg := &cfg.Staging

	if stg.Port == nil {
		stg.Port = intPtr(prod.Port)
	}
	if stg.Workers == nil {
		stg.Workers = strPtr(prod.Workers)
	}
	if stg.LogLevel == nil {
		stg.LogLevel = strPtr(prod.LogLevel)
	}
	if stg.ProcessManager == nil {
		stg.ProcessManager = strPtr(prod.ProcessManager)
	}
	if stg.TLS == nil {
		tls := prod.TLS // copy value, not pointer, to avoid aliasing
		stg.TLS = &tls
	}
	if stg.Proxy == nil {
		proxy := prod.Proxy // copy value
		stg.Proxy = &proxy
	}
}

// MergeLayers merges up to three ProjectConfig layers into a new ProjectConfig.
// Resolution order (highest priority wins): site > commonSite > project.
// Any layer may be nil and will be skipped.
//
// Non-zero override values replace base values. Zero values (empty string, 0,
// false) in an override layer are treated as "not set" and do not override a
// non-zero base. This matches the expected site-config override semantics for
// MS-01; pointer-based fields (e.g., StagingConfig, KafkaConfig.Enabled) use
// nil-checks instead.
func MergeLayers(project, commonSite, site *ProjectConfig) *ProjectConfig {
	result := deepCopyConfig(project)
	if result == nil {
		result = &ProjectConfig{}
	}
	if commonSite != nil {
		mergeInto(result, commonSite)
	}
	if site != nil {
		mergeInto(result, site)
	}
	return result
}

// mergeInto applies non-zero fields from override onto base in-place.
func mergeInto(base, override *ProjectConfig) {
	mergeString(&base.Moca, override.Moca)

	mergeString(&base.Project.Name, override.Project.Name)
	mergeString(&base.Project.Version, override.Project.Version)

	// Apps map: each key from override wins over the base entry.
	if len(override.Apps) > 0 {
		if base.Apps == nil {
			base.Apps = make(map[string]AppConfig, len(override.Apps))
		}
		for k, v := range override.Apps {
			base.Apps[k] = v
		}
	}

	mergeDatabase(&base.Infrastructure.Database, &override.Infrastructure.Database)
	mergeRedis(&base.Infrastructure.Redis, &override.Infrastructure.Redis)
	mergeKafka(&base.Infrastructure.Kafka, &override.Infrastructure.Kafka)
	mergeSearch(&base.Infrastructure.Search, &override.Infrastructure.Search)
	mergeStorage(&base.Infrastructure.Storage, &override.Infrastructure.Storage)

	mergeDevelopment(&base.Development, &override.Development)
	mergeProduction(&base.Production, &override.Production)
	mergeStaging(&base.Staging, &override.Staging)
	mergeScheduler(&base.Scheduler, &override.Scheduler)
	mergeBackup(&base.Backup, &override.Backup)
}

func mergeDatabase(base, override *DatabaseConfig) {
	mergeString(&base.Driver, override.Driver)
	mergeString(&base.Host, override.Host)
	mergeString(&base.SystemDB, override.SystemDB)
	mergeInt(&base.Port, override.Port)
	mergeInt(&base.PoolSize, override.PoolSize)
}

func mergeRedis(base, override *RedisConfig) {
	mergeString(&base.Host, override.Host)
	mergeInt(&base.Port, override.Port)
	mergeInt(&base.DbCache, override.DbCache)
	mergeInt(&base.DbQueue, override.DbQueue)
	mergeInt(&base.DbSession, override.DbSession)
	mergeInt(&base.DbPubSub, override.DbPubSub)
}

func mergeKafka(base, override *KafkaConfig) {
	if override.Enabled != nil {
		enabled := *override.Enabled
		base.Enabled = &enabled
	}
	if len(override.Brokers) > 0 {
		base.Brokers = make([]string, len(override.Brokers))
		copy(base.Brokers, override.Brokers)
	}
}

func mergeSearch(base, override *SearchConfig) {
	mergeString(&base.Engine, override.Engine)
	mergeString(&base.Host, override.Host)
	mergeString(&base.APIKey, override.APIKey)
	mergeInt(&base.Port, override.Port)
}

func mergeStorage(base, override *StorageConfig) {
	mergeString(&base.Driver, override.Driver)
	mergeString(&base.Endpoint, override.Endpoint)
	mergeString(&base.Bucket, override.Bucket)
	mergeString(&base.AccessKey, override.AccessKey)
	mergeString(&base.SecretKey, override.SecretKey)
}

func mergeDevelopment(base, override *DevelopmentConfig) {
	mergeInt(&base.Port, override.Port)
	mergeInt(&base.Workers, override.Workers)
	mergeBool(&base.AutoReload, override.AutoReload)
	mergeBool(&base.DeskDevServer, override.DeskDevServer)
	mergeInt(&base.DeskPort, override.DeskPort)
}

func mergeProduction(base, override *ProductionConfig) {
	mergeInt(&base.Port, override.Port)
	mergeString(&base.Workers, override.Workers)
	mergeString(&base.LogLevel, override.LogLevel)
	mergeString(&base.ProcessManager, override.ProcessManager)
	mergeString(&base.TLS.Provider, override.TLS.Provider)
	mergeString(&base.TLS.Email, override.TLS.Email)
	mergeString(&base.Proxy.Engine, override.Proxy.Engine)
}

func mergeStaging(base, override *StagingConfig) {
	mergeString(&base.Inherits, override.Inherits)
	if override.Port != nil {
		port := *override.Port
		base.Port = &port
	}
	if override.Workers != nil {
		w := *override.Workers
		base.Workers = &w
	}
	if override.LogLevel != nil {
		l := *override.LogLevel
		base.LogLevel = &l
	}
	if override.ProcessManager != nil {
		pm := *override.ProcessManager
		base.ProcessManager = &pm
	}
	if override.TLS != nil {
		tls := *override.TLS
		base.TLS = &tls
	}
	if override.Proxy != nil {
		proxy := *override.Proxy
		base.Proxy = &proxy
	}
}

func mergeScheduler(base, override *SchedulerConfig) {
	mergeBool(&base.Enabled, override.Enabled)
	mergeString(&base.TickInterval, override.TickInterval)
}

func mergeBackup(base, override *BackupConfig) {
	mergeString(&base.Schedule, override.Schedule)
	mergeBool(&base.Encrypt, override.Encrypt)
	mergeString(&base.EncryptionKey, override.EncryptionKey)
	mergeString(&base.Destination.Driver, override.Destination.Driver)
	mergeString(&base.Destination.Bucket, override.Destination.Bucket)
	mergeString(&base.Destination.Prefix, override.Destination.Prefix)
	mergeInt(&base.Retention.Daily, override.Retention.Daily)
	mergeInt(&base.Retention.Weekly, override.Retention.Weekly)
	mergeInt(&base.Retention.Monthly, override.Retention.Monthly)
}

// deepCopyConfig returns a deep copy of cfg, duplicating all maps, slices, and
// pointer fields so the copy is fully independent of the original.
func deepCopyConfig(cfg *ProjectConfig) *ProjectConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg // shallow copy of top-level struct

	// Deep copy Apps map.
	if cfg.Apps != nil {
		cp.Apps = make(map[string]AppConfig, len(cfg.Apps))
		for k, v := range cfg.Apps {
			cp.Apps[k] = v
		}
	}

	// Deep copy KafkaConfig.Enabled (*bool).
	if cfg.Infrastructure.Kafka.Enabled != nil {
		enabled := *cfg.Infrastructure.Kafka.Enabled
		cp.Infrastructure.Kafka.Enabled = &enabled
	}

	// Deep copy KafkaConfig.Brokers slice.
	if cfg.Infrastructure.Kafka.Brokers != nil {
		cp.Infrastructure.Kafka.Brokers = make([]string, len(cfg.Infrastructure.Kafka.Brokers))
		copy(cp.Infrastructure.Kafka.Brokers, cfg.Infrastructure.Kafka.Brokers)
	}

	// Deep copy StagingConfig pointer fields.
	if cfg.Staging.Port != nil {
		p := *cfg.Staging.Port
		cp.Staging.Port = &p
	}
	if cfg.Staging.Workers != nil {
		w := *cfg.Staging.Workers
		cp.Staging.Workers = &w
	}
	if cfg.Staging.LogLevel != nil {
		l := *cfg.Staging.LogLevel
		cp.Staging.LogLevel = &l
	}
	if cfg.Staging.ProcessManager != nil {
		pm := *cfg.Staging.ProcessManager
		cp.Staging.ProcessManager = &pm
	}
	if cfg.Staging.TLS != nil {
		tls := *cfg.Staging.TLS
		cp.Staging.TLS = &tls
	}
	if cfg.Staging.Proxy != nil {
		proxy := *cfg.Staging.Proxy
		cp.Staging.Proxy = &proxy
	}

	return &cp
}

// mergeString sets *base to override when override is non-empty.
func mergeString(base *string, override string) {
	if override != "" {
		*base = override
	}
}

// mergeInt sets *base to override when override is non-zero.
func mergeInt(base *int, override int) {
	if override != 0 {
		*base = override
	}
}

// mergeBool sets *base to true when override is true.
// Note: a false override cannot unset a true base; use pointer fields if needed.
func mergeBool(base *bool, override bool) {
	if override {
		*base = true
	}
}

// intPtr returns a pointer to a copy of v.
func intPtr(v int) *int { return &v }

// strPtr returns a pointer to a copy of v.
func strPtr(v string) *string { return &v }
