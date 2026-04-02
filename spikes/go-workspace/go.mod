module github.com/osama1998H/moca/spikes/go-workspace

go 1.26.1

require (
	github.com/osama1998H/moca/spikes/go-workspace/apps/stub-a v0.0.0
	github.com/osama1998H/moca/spikes/go-workspace/apps/stub-b v0.0.0
	golang.org/x/mod v0.22.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/osama1998H/moca/spikes/go-workspace/framework v0.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// replace directives enable standalone go mod tidy without workspace.
// When the workspace (go.work) is active, the 'use' directives take precedence.
replace (
	github.com/osama1998H/moca/spikes/go-workspace/apps/stub-a => ./apps/stub-a
	github.com/osama1998H/moca/spikes/go-workspace/apps/stub-b => ./apps/stub-b
	github.com/osama1998H/moca/spikes/go-workspace/framework => ./framework
)
