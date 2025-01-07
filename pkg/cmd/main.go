package cmd

import (
	_ "embed"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/utils"
)

//go:embed data/regions.list
var regionsList string

type Opts struct {
	Debug        bool
	Setup        bool
	Org          bool
	Profile      string
	Name         string
	RolesPath    string
	AccountsPath string
	AccountsStr  string
	Force        bool
	Clean        bool
	RateLimit    int
}

// LoadAllPlugins loads all enabled plugins.
//
// Add new plugins here.
func LoadAllPlugins(cfgs map[string]utils.ThreadConfig) [][]plugins.Plugin {
	return [][]plugins.Plugin{
		plugins.NewECRPublicRepositories(cfgs, 1),
		plugins.NewAccessPoints(cfgs, 1),
		plugins.NewS3Buckets(cfgs, 1),
		plugins.NewSNSTopics(cfgs, 1),
		plugins.NewSQSQueues(cfgs, 1),
	}
}
