package plugins

import "github.com/ryanjarv/roles/pkg/utils"

type Plugin interface {
	Name() string
	Setup(ctx *utils.Context) error
	ScanArn(ctx *utils.Context, arn string) (bool, error)
	CleanUp(ctx *utils.Context) error
}
