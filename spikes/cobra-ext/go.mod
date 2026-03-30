module github.com/moca-framework/moca/spikes/cobra-ext

go 1.26.1

require (
	github.com/moca-framework/moca/spikes/cobra-ext/apps/stub-a v0.0.0
	github.com/moca-framework/moca/spikes/cobra-ext/apps/stub-b v0.0.0
	github.com/moca-framework/moca/spikes/cobra-ext/framework v0.0.0
	github.com/spf13/cobra v1.8.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
)

replace (
	github.com/moca-framework/moca/spikes/cobra-ext/apps/stub-a => ./apps/stub-a
	github.com/moca-framework/moca/spikes/cobra-ext/apps/stub-b => ./apps/stub-b
	github.com/moca-framework/moca/spikes/cobra-ext/framework => ./framework
)
