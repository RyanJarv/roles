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
	"github.com/ryanjarv/roles/pkg/cmd"
	"github.com/ryanjarv/roles/pkg/utils"
	"os"
)

func main() {
	opts := cmd.Opts{}

	flag.BoolVar(&opts.Debug, "debug", false, "Enable debug logging")
	flag.BoolVar(&opts.Clean, "clean", false, "Cleanup")
	flag.StringVar(&opts.Profile, "profile", "", "AWS profile to use for scanning")
	flag.StringVar(&opts.Name, "name", "default", "Name of the scan")
	flag.StringVar(&opts.RolesPath, "roles", "", "Additional role names")
	flag.StringVar(&opts.AccountsPath, "account-list", "", "Path to a file containing account IDs")
	flag.StringVar(&opts.AccountsStr, "accounts", "", "Path to a file containing account IDs")
	flag.BoolVar(&opts.Force, "force", false, "Force rescan")
	flag.BoolVar(&opts.Setup, "setup", false, "Run optional one-time account optimization setup")
	flag.IntVar(&opts.RateLimit, "rate-limit", 100, "Roles scanned per second (default: 5, max: 1000)")

	flag.Parse()

	ctx := utils.NewContext(context.Background())

	if opts.Debug {
		ctx.Debug.SetOutput(os.Stderr)
	}

	if opts.Setup && opts.Clean {
		ctx.Error.Fatalf("cannot use both -setup and -clean")
	} else if opts.Org && !opts.Setup {
		ctx.Error.Fatalf("cannot use -org without -setup")
	} else if opts.RateLimit <= 0 || opts.RateLimit > 1000 {
		ctx.Error.Fatalf("rate-limit must be between 1 and 1000")
	} else if opts.Setup {
		// Run optional one-time account optimizer
		if err := cmd.Setup(ctx, opts.Profile, opts.Org); err != nil {
			ctx.Error.Fatalf("running: %s", err)
		}
	} else if opts.Clean {
		if err := cmd.CleanUp(ctx, opts); err != nil {
			ctx.Error.Fatalf("running: %s", err)
		}
	} else {
		if err := cmd.Run(ctx, opts); err != nil {
			ctx.Error.Fatalf("running: %s", err)
		}
	}
}
