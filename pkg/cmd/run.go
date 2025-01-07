package cmd

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/ryanjarv/roles/pkg/arn"
	"github.com/ryanjarv/roles/pkg/scanner"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"strings"
)

func Run(ctx *utils.Context, opts Opts) error {
	cfg, err := config.LoadDefaultConfig(ctx.Context,
		config.WithRegion("us-east-1"),
		config.WithSharedConfigProfile(opts.Profile),
		config.WithRetryMode(aws.RetryModeAdaptive),
	)
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

	storage, err := scanner.NewStorage(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("new storage: %s", err)
	}
	defer storage.Close()

	scan := scanner.NewScanner(&scanner.NewScannerInput{
		Storage:   storage,
		Force:     opts.Force,
		Plugins:   LoadAllPlugins(cfgs),
		RateLimit: opts.RateLimit,
	})

	scanData, err := arn.GetArns(ctx, &arn.GetArnsInput{
		AccountsStr:  opts.AccountsStr,
		AccountsPath: opts.AccountsPath,
		RolePaths:    strings.Split(opts.RolesPath, ","),
		Regions:      utils.GetInputFromPath(regionsList),
	})
	if err != nil {
		return fmt.Errorf("getting scanData: %s", err)
	}

	for principalArn, exists := range scan.ScanArns(ctx, lo.Keys(scanData)) {
		if exists {
			fmt.Println(principalArn, "#", scanData[principalArn].Comment)
		}
	}

	if err := storage.Save(); err != nil {
		return fmt.Errorf("saving storage: %s", err)
	}

	return nil
}
