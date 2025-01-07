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
	Concurrency  int
	Force        bool
	Clean        bool
}

// LoadAllPlugins loads all enabled plugins.
//
// Add new plugins here.
func LoadAllPlugins(cfgs map[string]utils.ThreadConfig) [][]plugins.Plugin {
	return [][]plugins.Plugin{
		//plugins.NewECRPublicRepositories(cfgs, 10),
		//plugins.NewAccessPoints(cfgs, 4),
		//plugins.NewS3Buckets(cfgs, 4),
		plugins.NewSNSTopics(cfgs, 1),
		plugins.NewSQSQueues(cfgs, 1),
	}
}
