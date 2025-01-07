package cmd

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	"github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/ryanjarv/roles/pkg/utils"
	"strconv"
	"sync"
	"time"
)

// Setup runs a one-time account optimization
func Setup(ctx *utils.Context, profile string, org bool) error {
	ctx.Info.Printf("Running one-time account optimization")

	cfg, err := config.LoadDefaultConfig(ctx.Context, config.WithRegion("us-east-1"), config.WithSharedConfigProfile(profile))
	if err != nil {
		return fmt.Errorf("loading config: %s", err)
	}

	if org {
		err = SetupOrg(ctx, cfg)
		if err != nil {
			return fmt.Errorf("setting up org: %s", err)
		}
	}

	accounts, err := utils.LoadAccounts(ctx, cfg)
	if err != nil {
		return fmt.Errorf("loading accounts: %s", err)
	}

	if err := SetupAccounts(ctx, accounts, cfg, err); err != nil {
		return fmt.Errorf("setting up accounts: %s", err)
	}

	return nil
}

func SetupAccounts(ctx *utils.Context, accounts map[string]utils.Account, cfg aws.Config, err error) error {
	wg := sync.WaitGroup{}

	// Run setup for each account
	for _, v := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := utils.EnableAllRegions(ctx, v.Svc.Account); err != nil {
				ctx.Error.Printf("enabling all regions: %s", err)
			}
		}()
	}

	wg.Wait()
	ctx.Info.Printf("Enabling all regions, this can take a while...")

	cfgs, err := utils.LoadConfigs(ctx, accounts)
	if err != nil {
		return fmt.Errorf("loading configs: %s", err)
	}

	if err := SetupPlugins(ctx, cfgs); err != nil {
		return fmt.Errorf("setting up plugins: %s", err)
	}

	return nil
}

// SetupPlugins calls Setup on each plugin for each thread config.
func SetupPlugins(ctx *utils.Context, cfgs map[string]utils.ThreadConfig) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %s", r)
		}
	}()

	wg := sync.WaitGroup{}

	// Setup 40 regions/accounts concurrently
	concurrent := make(chan int, 40)

	for _, plugin := range utils.FlattenList(LoadAllPlugins(cfgs)) {
		wg.Add(1)
		concurrent <- 1

		go func() {
			defer func() {
				wg.Done()
				<-concurrent
			}()
			ctx.Info.Printf("%s: setting up", plugin.Name())

			if err := plugin.Setup(ctx); err != nil {
				panic(fmt.Errorf("%s: %s", plugin.Name(), err))
			} else {
				ctx.Info.Printf("%s: setup complete", plugin.Name())
			}
		}()
	}

	// Wait for all plugins to finish setting up.
	wg.Wait()
	ctx.Info.Printf("Setting up plugins")

	return nil
}

// SetupOrg sets up an organization dedicated for scanning roles.
//
// This organization shouldn't be used for anything else. During setup, we create as many accounts as possible
// and enable all regions in each account. The org info is saved to disk so that it can use each account for scanning.
func SetupOrg(ctx *utils.Context, cfg aws.Config) error {
	ctx.Info.Printf("Setting up organization")

	// Create the organization
	email, err := CreateOrganization(ctx, cfg)
	if err != nil {
		return fmt.Errorf("creating organization: %s", err)
	}

	// Create accounts
	return CreateAccounts(ctx, cfg, email)
}

func CreateAccounts(ctx *utils.Context, cfg aws.Config, email string) error {
	svc := organizations.NewFromConfig(cfg)

	for i := 1; i < 100; i++ {
		postfix := utils.RandStringRunes(8)

		createResp, err := svc.CreateAccount(ctx, &organizations.CreateAccountInput{
			AccountName: aws.String(fmt.Sprintf("role-scanning-sub-account-%s", postfix)),
			Email:       aws.String(utils.GenerateSubAccountEmail(email, postfix)),
			RoleName:    aws.String("OrganizationAccountAccessRole"),
			Tags: []types.Tag{
				{
					Key:   aws.String("role-scanning-account"),
					Value: aws.String("true"),
				},
				{
					Key:   aws.String("role-scanning-account-number"),
					Value: aws.String(strconv.Itoa(i)),
				},
			},
		})

		var throttled *types.TooManyRequestsException
		var maxAccounts *types.ConstraintViolationException

		if errors.As(err, &throttled) {
			ctx.Info.Printf("Rate limited, waiting 5 seconds")
			time.Sleep(5 * time.Second)
		} else if errors.As(err, &maxAccounts) {
			ctx.Info.Printf("Max accounts reached")
			break
		} else if err != nil {
			return fmt.Errorf("creating account: %s", err)
		}

		resp, err := svc.DescribeCreateAccountStatus(ctx, &organizations.DescribeCreateAccountStatusInput{
			CreateAccountRequestId: createResp.CreateAccountStatus.Id,
		})
		if err != nil {
			return fmt.Errorf("describing account: %s", err)
		}

		for resp.CreateAccountStatus.State == types.CreateAccountStateInProgress {
			time.Sleep(3 * time.Second)

			resp, err = svc.DescribeCreateAccountStatus(ctx, &organizations.DescribeCreateAccountStatusInput{
				CreateAccountRequestId: createResp.CreateAccountStatus.Id,
			})
			if err != nil {
				return fmt.Errorf("describing account: %s", err)
			}
		}

		if resp.CreateAccountStatus.State == types.CreateAccountStateFailed {
			return fmt.Errorf("account creation failed: %s", string(resp.CreateAccountStatus.FailureReason))
		}

		ctx.Info.Printf("Created account %s", *createResp.CreateAccountStatus.AccountName)
	}

	return nil
}

func CreateOrganization(ctx *utils.Context, cfg aws.Config) (string, error) {
	svc := organizations.NewFromConfig(cfg)

	var alreadyCreated *types.AlreadyInOrganizationException

	if resp, err := svc.CreateOrganization(ctx, &organizations.CreateOrganizationInput{
		FeatureSet: types.OrganizationFeatureSetAll,
	}); errors.As(err, &alreadyCreated) {
		ctx.Debug.Printf("Organization already created")
	} else if err != nil {
		return "", fmt.Errorf("creating organization: %s", err)
	} else {
		return *resp.Organization.MasterAccountEmail, nil
	}

	if resp, err := svc.DescribeOrganization(ctx, &organizations.DescribeOrganizationInput{}); err != nil {
		return "", fmt.Errorf("describing organization: %s", err)
	} else {
		return *resp.Organization.MasterAccountEmail, nil
	}
}
