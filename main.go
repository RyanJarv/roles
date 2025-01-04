// Package roles does unauthenticated enumeration of IAM role ARNs. The goal is to find all variations of specific roles
// in a given account rather than across a list of roles in all accounts like quiet riot does. Some role roles include
// random prefixes so we want to guess those as well if possible.
//
// The roles we are looking for may include placeholders like {accountId} or {region} which we may already know.
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/ryanjarv/roles/pkg/arn"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/scanner"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"os"
	"strings"
)

//go:embed data/regions.list
var regionsList string

type Opts struct {
	debug        bool
	profile      string
	name         string
	rolesPath    string
	accountsPath string
	accountsStr  string
	concurrency  int
	force        bool
	clean        bool
}

func main() {
	opts := Opts{}

	flag.BoolVar(&opts.debug, "debug", false, "Enable debug logging")
	flag.BoolVar(&opts.clean, "clean", false, "Cleanup")
	flag.StringVar(&opts.profile, "profile", "", "AWS profile to use for scanning")
	flag.StringVar(&opts.name, "name", "default", "Name of the scan")
	flag.StringVar(&opts.rolesPath, "roles", "", "Additional role names")
	flag.StringVar(&opts.accountsPath, "account-list", "", "Path to a file containing account IDs")
	flag.StringVar(&opts.accountsStr, "accounts", "", "Path to a file containing account IDs")
	flag.IntVar(&opts.concurrency, "concurrency", 2, "Scanner concurrency")
	flag.BoolVar(&opts.force, "force", false, "Force rescan")

	flag.Parse()

	ctx := utils.NewContext(context.Background())

	if opts.debug {
		ctx.Debug.SetOutput(os.Stderr)
	}

	if err := Run(ctx, opts); err != nil {
		ctx.Error.Fatalf("running: %s", err)
	}
}

func Run(ctx *utils.Context, opts Opts) error {
	cfgs, caller, err := LoadConfigs(ctx, opts.profile)
	if err != nil {
		return fmt.Errorf("loading configs: %s", err)
	}

	storage, err := scanner.NewStorage(ctx, opts.name)
	if err != nil {
		return fmt.Errorf("new storage: %s", err)
	}
	defer storage.Close()

	scan := scanner.NewScanner(&scanner.NewScannerInput{
		Concurrency: opts.concurrency,
		Storage:     storage,
		Force:       opts.force,
		Plugins: [][]plugins.Plugin{
			plugins.NewAccessPoints(cfgs, plugins.NewAccessPointInput{
				AccountId: *caller.Account,
			}),
		},
	})

	if opts.clean {
		if err := scan.CleanUp(ctx); err != nil {
			return fmt.Errorf("cleaning up: %s", err)
		}
		return nil
	}

	scan.SetupPlugins(ctx)

	scanData, err := arn.GetArns(ctx, &arn.GetArnsInput{
		AccountsStr:  opts.accountsStr,
		AccountsPath: opts.accountsPath,
		RolePaths:    strings.Split(opts.rolesPath, ","),
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

func LoadConfigs(ctx *utils.Context, profile string) (map[string]aws.Config, *sts.GetCallerIdentityOutput, error) {
	cfg, err := config.LoadDefaultConfig(ctx.Context, config.WithRegion("us-east-1"), config.WithSharedConfigProfile(profile))
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %s", err)
	}

	caller, err := utils.GetCallerInfo(ctx, cfg)
	if err != nil {
		return nil, caller, fmt.Errorf("getting caller info: %s", err)
	}

	regions, err := ec2.NewFromConfig(cfg).DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, nil, err
	}

	cfgs := make(map[string]aws.Config, len(regions.Regions))
	for _, region := range regions.Regions {
		cfgCopy := cfg.Copy()
		cfgCopy.Region = *region.RegionName
		cfgs[*region.RegionName] = cfgCopy
	}

	ctx.Debug.Printf("callerArn: %s, accountId: %s", *caller.Arn, *caller.Account)

	return cfgs, caller, nil
}
