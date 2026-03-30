module github.com/moca-framework/moca/spikes/go-workspace

go 1.26.1

require (
	github.com/moca-framework/moca/spikes/go-workspace/apps/stub-a v0.0.0
	github.com/moca-framework/moca/spikes/go-workspace/apps/stub-b v0.0.0
	golang.org/x/mod v0.22.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/moca-framework/moca/spikes/go-workspace/framework v0.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// replace directives enable standalone go mod tidy without workspace.
// When the workspace (go.work) is active, the 'use' directives take precedence.
replace (
	github.com/moca-framework/moca/spikes/go-workspace/apps/stub-a => ./apps/stub-a
	github.com/moca-framework/moca/spikes/go-workspace/apps/stub-b => ./apps/stub-b
	github.com/moca-framework/moca/spikes/go-workspace/framework => ./framework
)
