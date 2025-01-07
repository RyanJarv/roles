package utils

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/account"
	"github.com/aws/aws-sdk-go-v2/service/account/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"strings"
	"sync"
	"time"
)

type ThreadConfig struct {
	AccountId string
	Config    aws.Config
	Region    string
}

func LoadConfigs(ctx *Context, accounts map[string]Account) (map[string]ThreadConfig, error) {
	cfgs := map[string]ThreadConfig{}
	m := &sync.Mutex{}

	wg := &sync.WaitGroup{}
	errs := make(chan error)

	for _, v := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()

			regions, err := GetAllEnabledRegions(ctx, v.Svc.Account)
			if err != nil {
				errs <- fmt.Errorf("getting enabled regions: %s", err)
				return
			}

			for _, region := range regions {
				newCfg := v.Config.Copy()
				newCfg.Region = *region.RegionName

				m.Lock()
				cfgs[fmt.Sprintf("%s-%s", v.AccountId, *region.RegionName)] = ThreadConfig{
					AccountId: v.AccountId,
					Config:    newCfg,
					Region:    *region.RegionName,
				}
				m.Unlock()
			}

			ctx.Info.Printf("loaded %d regions in account %s", len(regions), v.AccountId)
		}()
	}
	wg.Wait()

	if err := CheckErrorCh(errs); err != nil {
		return nil, err
	}

	return cfgs, nil
}

func GetAllEnabledRegions(ctx *Context, svc *account.Client) ([]types.Region, error) {
	var regions []types.Region
	paginator := account.NewListRegionsPaginator(svc, &account.ListRegionsInput{
		MaxResults: aws.Int32(50),
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

func EnableAllRegions(ctx *Context, svc *account.Client) error {
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
