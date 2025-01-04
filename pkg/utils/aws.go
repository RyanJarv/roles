package utils

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/account"
	"github.com/aws/aws-sdk-go-v2/service/account/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"time"
)

func LoadConfigs(ctx *Context, profile string) (map[string]aws.Config, *sts.GetCallerIdentityOutput, error) {
	cfg, err := config.LoadDefaultConfig(ctx.Context, config.WithRegion("us-east-1"), config.WithSharedConfigProfile(profile))
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %s", err)
	}

	caller, err := GetCallerInfo(ctx, cfg)
	if err != nil {
		return nil, caller, fmt.Errorf("getting caller info: %s", err)
	}

	regions, err := GetAllEnabledRegions(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("getting enabled regions: %s", err)
	}

	cfgs := make(map[string]aws.Config, len(regions))
	for _, region := range regions {
		cfgCopy := cfg.Copy()
		cfgCopy.Region = *region.RegionName
		cfgs[*region.RegionName] = cfgCopy
	}

	ctx.Debug.Printf("callerArn: %s, accountId: %s", *caller.Arn, *caller.Account)

	return cfgs, caller, nil
}

func GetAllEnabledRegions(ctx *Context, cfg aws.Config) ([]types.Region, error) {
	var regions []types.Region
	svc := account.NewFromConfig(cfg)
	paginator := account.NewListRegionsPaginator(svc, &account.ListRegionsInput{
		RegionOptStatusContains: []types.RegionOptStatus{
			types.RegionOptStatusEnabled,
			types.RegionOptStatusEnabledByDefault,
		},
	})
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing regions: %s", err)
		}
		regions = append(regions, resp.Regions...)
	}
	ctx.Debug.Printf("Found %d enabled regions", len(regions))
	return regions, nil
}

func GetCallerInfo(ctx *Context, cfg aws.Config) (*sts.GetCallerIdentityOutput, error) {
	resp, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("getting caller identity: %w", err)
	}
	return resp, nil
}

func EnableAllRegions(ctx *Context, cfg aws.Config) error {
	svc := account.NewFromConfig(cfg)
	paginator := account.NewListRegionsPaginator(svc, &account.ListRegionsInput{
		RegionOptStatusContains: []types.RegionOptStatus{
			types.RegionOptStatusDisabled,
		},
	})
	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describing regions: %s", err)
		}

		for _, region := range resp.Regions {
			for {
				ctx.Info.Printf("Opting in to region %s", *region.RegionName)

				var toManyReqs *types.TooManyRequestsException

				if _, err := svc.EnableRegion(ctx, &account.EnableRegionInput{
					RegionName: region.RegionName,
				}); errors.As(err, &toManyReqs) {
					ctx.Info.Printf("Too many requests, sleeping for 10 seconds")
					time.Sleep(10 * time.Second)
					continue
				} else if err != nil {
					return fmt.Errorf("enabling region %s: %s", *region.RegionName, err)
				} else {
					break
				}
			}
		}
	}

	for {
		time.Sleep(2 * time.Second)

		if resp, err := svc.ListRegions(ctx, &account.ListRegionsInput{
			RegionOptStatusContains: []types.RegionOptStatus{
				types.RegionOptStatusEnabling,
			},
		}); err != nil {
			return fmt.Errorf("describing regions: %s", err)
		} else if len(resp.Regions) == 0 {
			break
		} else {
			ctx.Info.Printf("Waiting for %d regions to finish enabling", len(resp.Regions))
		}
	}
	return nil
}

func GenerateTrustPolicy(resourceArn string, arn string) PolicyDocument {
	document := PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Sid:      "testrole",
				Effect:   "Deny",
				Action:   "*",
				Resource: resourceArn,
				Principal: PolicyPrincipal{
					AWS: arn,
				},
			},
		},
	}
	return document
}
