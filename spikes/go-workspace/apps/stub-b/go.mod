module github.com/moca-framework/moca/spikes/go-workspace/apps/stub-b

go 1.26.1

require (
	github.com/moca-framework/moca/spikes/go-workspace/framework v0.0.0
	github.com/stretchr/testify v1.9.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// replace directive enables standalone go mod tidy without workspace.
// When the workspace (go.work) is active, the 'use ./framework' directive
// takes precedence and this replace is effectively overridden.
replace github.com/moca-framework/moca/spikes/go-workspace/framework => ../../framework
