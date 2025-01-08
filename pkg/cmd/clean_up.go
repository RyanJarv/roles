package cmd

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ryanjarv/roles/pkg/utils"
	"sync"
)

func CleanUp(ctx *utils.Context, opts Opts) error {
	cfg, err := config.LoadDefaultConfig(ctx.Context, config.WithRegion("us-east-1"), config.WithSharedConfigProfile(opts.Profile))
	if err != nil {
		return fmt.Errorf("loading config: %s", err)
	}

	accounts, err := utils.LoadAccounts(ctx, cfg)
	if err != nil {
		return fmt.Errorf("loading accounts: %s", err)
	}

	cfgs, err := utils.LoadConfigs(ctx, accounts)
	if err != nil {
		return fmt.Errorf("loading configs: %s", err)
	}

	if err := cleanUp(ctx, cfgs); err != nil {
		return fmt.Errorf("cleaning up: %s", err)
	}

	return nil
}

func cleanUp(ctx *utils.Context, cfgs map[string]utils.ThreadConfig) (err error) {
	concurrency := make(chan int, 20)
	wg := sync.WaitGroup{}

	for _, p := range utils.FlattenList(LoadAllPlugins(cfgs)) {
		wg.Add(1)
		concurrency <- 1

		go func() {
			defer func() {
				<-concurrency
				wg.Done()
			}()

			ctx.Info.Printf("cleaning up %s", p.Name())
			if err := p.CleanUp(ctx); err != nil {
				ctx.Error.Printf("%s: cleaning up: %s", p.Name(), err)
			}
		}()
	}

	wg.Wait()
	return nil
}
