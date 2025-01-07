package utils

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type Account struct {
	RoleArn     string     `json:"role_arn"`
	AccountId   string     `json:"account_id"`
	AccountName string     `json:"account_name"`
	Config      aws.Config `json:"-"`
}

func LoadAccounts(ctx *Context, cfg aws.Config) (map[string]Account, error) {
	svc := organizations.NewFromConfig(cfg)
	paginator := organizations.NewListAccountsPaginator(svc, &organizations.ListAccountsInput{})

	info, err := GetCallerInfo(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("getting caller info: %s", err)
	}

	accounts := map[string]Account{
		// Always add the current account, it won't have tags set and may not even be an organization account
		"root": {
			AccountId:   *info.Account,
			AccountName: "root",
			Config:      cfg,
		},
	}

	for paginator.HasMorePages() {
		resp, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing accounts: %s", err)
		}

		for _, account := range resp.Accounts {
			resp, err := svc.ListTagsForResource(ctx, &organizations.ListTagsForResourceInput{
				ResourceId: account.Id,
			})
			if err != nil {
				return nil, fmt.Errorf("listing tags: %s", err)
			}
			if !HasTag(resp.Tags, "role-scanning-account", "true") {
				continue
			}

			roleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s", *account.Id, "OrganizationAccountAccessRole")

			accounts[*account.Id] = Account{
				RoleArn:     roleArn,
				AccountId:   *account.Id,
				AccountName: *account.Name,
				Config:      AssumeRoleConfig(cfg, roleArn),
			}

			ctx.Info.Printf("Found account %s", *account.Name)
		}
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
