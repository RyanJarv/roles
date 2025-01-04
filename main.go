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
	setup        bool
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
	flag.BoolVar(&opts.setup, "setup", false, "Run optional one-time account optimization setup")

	flag.Parse()

	ctx := utils.NewContext(context.Background())

	if opts.debug {
		ctx.Debug.SetOutput(os.Stderr)
	}

	if opts.setup && opts.clean {
		ctx.Error.Fatalf("cannot use both --setup and --clean")
	} else if opts.setup {
		// Run optional one-time account optimizer
		if err := Setup(ctx, opts); err != nil {
			ctx.Error.Fatalf("running: %s", err)
		}
	} else if opts.clean {
		if err := CleanUp(ctx, opts); err != nil {
			ctx.Error.Fatalf("running: %s", err)
		}
	} else {
		if err := Run(ctx, opts); err != nil {
			ctx.Error.Fatalf("running: %s", err)
		}
	}
}

// LoadAllPlugins loads all enabled plugins.
//
// Add new plugins here.
func LoadAllPlugins(cfgs map[string]aws.Config, caller *sts.GetCallerIdentityOutput) [][]plugins.Plugin {
	return [][]plugins.Plugin{
		plugins.NewAccessPoints(cfgs, plugins.NewAccessPointInput{
			AccountId: *caller.Account,
		}),
	}
}

func Run(ctx *utils.Context, opts Opts) error {
	cfgs, caller, err := utils.LoadConfigs(ctx, opts.profile)
	if err != nil {
		return fmt.Errorf("loading configs: %s", err)
	}

	storage, err := scanner.NewStorage(ctx, opts.name)
	if err != nil {
		return fmt.Errorf("new storage: %s", err)
	}
	defer storage.Close()

	scan := scanner.NewScanner(&scanner.NewScannerInput{
		Storage: storage,
		Force:   opts.force,
		Plugins: LoadAllPlugins(cfgs, caller),
	})

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

func CleanUp(ctx *utils.Context, opts Opts) error {
	cfgs, caller, err := utils.LoadConfigs(ctx, opts.profile)
	if err != nil {
		return fmt.Errorf("loading configs: %s", err)
	}

	scan := scanner.NewScanner(&scanner.NewScannerInput{
		Plugins: LoadAllPlugins(cfgs, caller),
	})

	if err := scan.CleanUp(ctx); err != nil {
		return fmt.Errorf("cleaning up: %s", err)
	}
	return nil
}

// Setup runs a one-time account optimization
func Setup(ctx *utils.Context, opts Opts) error {
	ctx.Info.Printf("Running one-time account optimization")

	cfg, err := config.LoadDefaultConfig(ctx.Context, config.WithRegion("us-east-1"), config.WithSharedConfigProfile(opts.profile))
	if err != nil {
		return fmt.Errorf("loading config: %s", err)
	}

	ctx.Info.Printf("Enabling all regions, this can take a while...")

	if err := utils.EnableAllRegions(ctx, cfg); err != nil {
		return fmt.Errorf("enabling all regions: %s", err)
	}

	cfgs, caller, err := utils.LoadConfigs(ctx, opts.profile)
	if err != nil {
		return fmt.Errorf("loading configs: %s", err)
	}

	scan := scanner.NewScanner(&scanner.NewScannerInput{
		Force:   opts.force,
		Plugins: LoadAllPlugins(cfgs, caller),
	})

	scan.SetupPlugins(ctx)

	return nil
}
