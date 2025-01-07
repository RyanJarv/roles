package utils

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/account"
	"github.com/aws/aws-sdk-go-v2/service/account/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"strings"
	"time"
)

type ThreadConfig struct {
	AccountId string
	Config    aws.Config
	Region    string
}

func LoadConfigs(ctx *Context, accounts map[string]Account) (map[string]ThreadConfig, error) {
	cfgs := map[string]ThreadConfig{}

	for _, v := range accounts {
		regions, err := GetAllEnabledRegions(ctx, v.Config)
		if err != nil {
			return nil, fmt.Errorf("getting enabled regions: %s", err)
		}

		for _, region := range regions {
			newCfg := v.Config.Copy()
			newCfg.Region = *region.RegionName
			cfgs[fmt.Sprintf("%s-%s", v.AccountId, *region.RegionName)] = ThreadConfig{
				AccountId: v.AccountId,
				Config:    newCfg,
				Region:    *region.RegionName,
			}
		}

		ctx.Info.Printf("loaded %d regions in account %s", len(regions), v.AccountId)
	}

	return cfgs, nil
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

func GenerateTrustPolicy(resourceArn, action, principalArn string) PolicyDocument {
	return PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Sid:      "testrole",
				Effect:   "Deny",
				Action:   action,
				Resource: resourceArn,
				Principal: PolicyPrincipal{
					AWS: principalArn,
				},
			},
		},
	}
}

func GenerateSubAccountEmail(email string, postfix string) string {
	p := strings.Split(email, "@")
	if len(p) != 2 {
		// This probably shouldn't happen.
		panic(fmt.Errorf("invalid email: %s", email))
	}
	if strings.Contains(p[0], "+") {
		return fmt.Sprintf("%s-%s@%s", p[0], postfix, p[1])
	} else {
		return fmt.Sprintf("%s+%s@%s", p[0], postfix, p[1])
	}
}
