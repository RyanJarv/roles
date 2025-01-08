package utils

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/account"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"sync"
)

type Svc struct {
	Organizations *organizations.Client
	STS           *sts.Client
	Account       *account.Client
}

type Account struct {
	RoleArn     string     `json:"role_arn"`
	AccountId   string     `json:"account_id"`
	AccountName string     `json:"account_name"`
	Config      aws.Config `json:"-"`
	Svc         Svc
}

func LoadAccounts(ctx *Context, cfg aws.Config) (map[string]Account, error) {
	svc := organizations.NewFromConfig(cfg)

	info, err := GetCallerInfo(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("getting caller info: %s", err)
	}

	accounts := map[string]Account{
		// Always add the current account, it won't have tags set and may not be an organization account.
		"default": {
			AccountId:   *info.Account,
			AccountName: "default",
			Config:      cfg,
			Svc: Svc{
				Organizations: organizations.NewFromConfig(cfg),
				STS:           sts.NewFromConfig(cfg),
				Account:       account.NewFromConfig(cfg),
			},
		},
	}

	paginator := organizations.NewListAccountsPaginator(svc, &organizations.ListAccountsInput{})
	wg := sync.WaitGroup{}
	errs := make(chan error)

	for paginator.HasMorePages() {
		var accessDenied *types.AccessDeniedException
		resp, err := paginator.NextPage(ctx)
		if errors.As(err, &accessDenied) {
			ctx.Debug.Printf("Access denied listing accounts, will use non-org mode.")
			return accounts, nil
		} else if err != nil {
			return nil, fmt.Errorf("listing accounts: %s", err)
		}

		for _, accnt := range resp.Accounts {
			wg.Add(1)

			go func() {
				defer wg.Done()

				resp, err := svc.ListTagsForResource(ctx, &organizations.ListTagsForResourceInput{
					ResourceId: accnt.Id,
				})
				if err != nil {
					errs <- fmt.Errorf("listing tags: %s", err)
				}
				if !HasTag(resp.Tags, "role-scanning-account", "true") {
					return
				}

				roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", *accnt.Id, "OrganizationAccountAccessRole")

				cfg := AssumeRoleConfig(cfg, roleArn)
				accounts[*accnt.Id] = Account{
					RoleArn:     roleArn,
					AccountId:   *accnt.Id,
					AccountName: *accnt.Name,
					Config:      cfg,
					Svc: Svc{
						Organizations: organizations.NewFromConfig(cfg),
						STS:           sts.NewFromConfig(cfg),
						Account:       account.NewFromConfig(cfg),
					},
				}

				ctx.Info.Printf("Found account %s", *accnt.Name)
			}()
		}
	}
	wg.Wait()

	if err := CheckErrorCh(errs); err != nil {
		return nil, err
	}

	return accounts, nil
}

func AssumeRoleConfig(cfg aws.Config, roleArn string) aws.Config {
	newCfg := cfg.Copy()
	newCfg.Credentials = aws.NewCredentialsCache(
		stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), roleArn, func(o *stscreds.AssumeRoleOptions) {
			o.RoleSessionName = "role-scanner"
			//o.Duration = 3600
		}),
	)
	return newCfg
}

func HasTag(tags []types.Tag, key string, value string) bool {
	for _, tag := range tags {
		if *tag.Key == key && *tag.Value == value {
			return true
		}
	}
	return false
}
