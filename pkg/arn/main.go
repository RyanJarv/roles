package arn

import (
	"bytes"
	"fmt"
	"github.com/ryanjarv/roles/pkg/utils"
	"strings"
	"text/template"
)

type GetArnsInput struct {
	RolePaths      []string
	PrincipalPaths []string
	Regions        map[string]utils.Info
	ForceScan      bool
	AccountsStr    string
	AccountsPath   string
}

func GetArns(ctx *utils.Context, input *GetArnsInput) (map[string]utils.Info, error) {
	var accounts map[string]utils.Info

	if input.AccountsPath != "" {
		var err error
		accounts, err = utils.GetInput(input.AccountsPath)
		if err != nil {
			ctx.Error.Fatalf("accounts: %s", err)
		}
	} else {
		accounts = map[string]utils.Info{}
	}

	for _, value := range strings.Split(input.AccountsStr, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		accounts[value] = utils.Info{}
	}

	roles, err := getRoleInputs(input.RolePaths)
	if err != nil {
		return nil, fmt.Errorf("getting allRoles: %s", err)
	}

	principals, err := getPrincipalInputs(input.PrincipalPaths)
	if err != nil {
		return nil, fmt.Errorf("getting allPrincipals: %s", err)
	}

	for name, info := range principals {
		roles[name] = info
	}

	result := map[string]utils.Info{}
	for account, accountInfo := range accounts {
		result[utils.GetRootArn(account)] = accountInfo

		for tmpl, roleInfo := range roles {
			for region, _ := range input.Regions {
				ctx.Debug.Printf("template %s - account %s - region %s", tmpl, account, region)

				arn, err := GetArn(tmpl, account, region)
				if err != nil {
					return nil, fmt.Errorf("GetArn: %s", err)
				}

				if accountInfo.Comment == "" {
					ctx.Debug.Printf("account %s has no comment", account)
				}

				result[arn] = utils.Info{
					Comment: accountInfo.Comment + " - " + roleInfo.Comment,
				}
			}
		}
	}

	return result, nil
}

type roleData struct {
	AccountId string
	Region    string
}

func getRoleInputs(paths []string) (map[string]utils.Info, error) {
	roles, err := utils.GetInput(paths...)
	if err != nil {
		return nil, err
	}

	result := map[string]utils.Info{}
	for role, info := range roles {
		result["role/"+role] = info
	}

	return result, nil
}

func getPrincipalInputs(paths []string) (map[string]utils.Info, error) {
	principals, err := utils.GetInput(paths...)
	if err != nil {
		return nil, err
	}

	for principal := range principals {
		if !strings.HasPrefix(principal, "role/") && !strings.HasPrefix(principal, "user/") {
			return nil, fmt.Errorf("principal %q must start with role/ or user/", principal)
		}
	}

	return principals, nil
}

// GetArn returns a list of ARNs based on the given template, account, and region
//
// Example:
//
//	role/cdk-hnb659fds-deploy-role-{{AccountId}}-{{region}}" -> [
//			"arn:aws:iam::123456789012:role/cdk-hnb659fds-deploy-role-123456789012-us-west-2"
//	]
func GetArn(principal string, account string, region string) (string, error) {
	tmpl, err := template.New(principal).Parse(principal)
	if err != nil {
		return "", err
	}

	data := roleData{
		AccountId: account,
		Region:    region,
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	return fmt.Sprintf("arn:aws:iam::%s:%s", account, buf.String()), err
}
