package factory

import "github.com/osama1998H/moca/pkg/meta"

// Option configures a DocFactory at construction time.
type Option func(*factoryConfig)

type factoryConfig struct {
	seed int64
}

// WithSeed sets the random seed for reproducible data generation.
func WithSeed(seed int64) Option {
	return func(c *factoryConfig) {
		c.seed = seed
	}
}

// GenOption configures a single Generate or GenerateAndInsert call.
type GenOption func(*genConfig)

type genConfig struct {
	overrides     map[string]any
	withChildren  bool
	childCountMin int
	childCountMax int
}

func defaultGenConfig() *genConfig {
	return &genConfig{
		withChildren:  true,
		childCountMin: 1,
		childCountMax: 5,
	}
}

// WithOverrides sets fixed values for specific fields, bypassing generation.
func WithOverrides(overrides map[string]any) GenOption {
	return func(c *genConfig) {
		c.overrides = overrides
	}
}

// WithChildren enables or disables child table row generation.
func WithChildren(enabled bool) GenOption {
	return func(c *genConfig) {
		c.withChildren = enabled
	}
}

// WithChildCount sets the range of child rows to generate per Table field.
func WithChildCount(min, max int) GenOption {
	return func(c *genConfig) {
		c.childCountMin = min
		c.childCountMax = max
	}
}

// FieldOverride allows per-field generator customization.
type FieldOverride struct {
	Value     any
	FieldName string
}

// fieldGenContext holds runtime state for generating a single field value.
type fieldGenContext struct {
	field     meta.FieldDef
	seq       int  // sequence number within the batch
	forceUniq bool // append unique suffix
}
