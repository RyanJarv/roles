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
	"github.com/ryanjarv/roles/pkg/arn"
	"github.com/ryanjarv/roles/pkg/scanner"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"os"
	"strings"
)

//go:embed data/regions.list
var regionsList string

var (
	debug        = flag.Bool("debug", false, "Enable debug logging")
	region       = flag.String("region", "", "AWS region to use for scanning")
	profile      = flag.String("profile", "", "AWS profile to use for scanning")
	name         = flag.String("name", "default", "Name of the scan")
	rolesPath    = flag.String("roles", "", "Additional role names")
	accountsPath = flag.String("account-list", "", "Path to a file containing account IDs")
	accountsStr  = flag.String("accounts", "", "Path to a file containing account IDs")
	concurrency  = flag.Int("concurrency", 2, "Scanner concurrency")
	force        = flag.Bool("force", false, "Force rescan")
)

func main() {
	flag.Parse()
	ctx := utils.NewContext(context.Background())
	if *debug {
		ctx.Debug.SetOutput(os.Stderr)
	}
	config, err := config.LoadDefaultConfig(ctx.Context, config.WithRegion(*region), config.WithSharedConfigProfile(*profile))
	if err != nil {
		ctx.Error.Fatalf("loading config: %s", err)
	}

	var accounts map[string]utils.Info

	if *accountsPath != "" {
		accounts, err = utils.GetInput(*accountsPath)
		if err != nil {
			ctx.Error.Fatalf("accounts: %s", err)
		}
	} else {
		accounts = map[string]utils.Info{}
	}

	for _, value := range strings.Split(*accountsStr, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		accounts[value] = utils.Info{}
	}

	if *rolesPath == "" {
		ctx.Error.Fatalf("-roles not provided")
	}
	rolePaths := strings.Split(*rolesPath, ",")

	if err := Scan(ctx, config, *name, *concurrency, &arn.GetArnsInput{
		RolePaths: rolePaths,
		Accounts:  accounts,
		Regions:   utils.GetInputFromPath(regionsList),
		ForceScan: *force,
	}); err != nil {
		ctx.Error.Fatalf("scanning: %s", err)
	}
}

func Scan(ctx *utils.Context, config aws.Config, name string, concurrency int, input *arn.GetArnsInput) error {
	accounts, err := arn.GetArns(ctx, input)
	if err != nil {
		return fmt.Errorf("getting arns: %s", err)
	}

	s, err := scanner.NewScanner(&scanner.NewScannerInput{
		Name:        name,
		Config:      config,
		Concurrency: concurrency,
	})
	if err != nil {
		return err
	}

	for account, scan := range accounts {
		all, err := utils.Load(name, account)
		if err != nil {
			return fmt.Errorf("loading: %s", err)
		}
		if !input.ForceScan {
			for k, info := range all {
				item, ok := scan[k]
				if !ok {
					continue
				}
				if info.Exists {
					ctx.Info.Printf("found in previous run: %s - %s", k, item.Comment)
				}
				delete(scan, k)
			}
		}

		rootArn := arn.GetRootArn(account)
		if info, ok := all[rootArn]; !ok || info.Exists {
			ok, err := s.ScanArn(ctx, rootArn)
			all[rootArn] = utils.Info{
				Exists: ok,
			}

			if err != nil {
				return fmt.Errorf("scanning root: %s", err)
			} else if !ok && !input.ForceScan {
				ctx.Info.Printf("skipping account: %s", rootArn)
				continue
			}
		}

		ctx.Info.Printf("scanning %d roles in account %s", len(scan), rootArn)

		for arn, exists := range s.ScanArns(ctx, lo.Keys(scan)...) {
			if exists {
				fmt.Println(arn, "#", scan[arn].Comment)
			}

			all[arn] = utils.Info{
				Comment: scan[arn].Comment,
				Exists:  exists,
			}
			//info := all[arn]
			//info.Exists = exists
			//all[arn] = info
		}

		utils.Save(ctx, name, account, all)
	}

	return nil
}
