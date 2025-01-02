package arn

import (
	"bytes"
	"fmt"
	"github.com/ryanjarv/roles/pkg/utils"
	"text/template"
)

type GetArnsInput struct {
	RolePaths []string
	Regions   map[string]utils.Info
	Accounts  map[string]utils.Info
	ForceScan bool
}

func GetArns(ctx *utils.Context, input *GetArnsInput) (map[string]map[string]utils.Info, error) {
	roles, err := utils.GetInput(input.RolePaths...)
	if err != nil {
		return nil, fmt.Errorf("getting allRoles: %s", err)
	}

	result := map[string]map[string]utils.Info{}
	for account, accountInfo := range input.Accounts {
		if _, ok := result[account]; !ok {
			result[account] = map[string]utils.Info{}
		}

		//if _, ok := result[account]; !ok {
		//	result[account] = map[string]utils.Info{}
		//}

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

				result[account][arn] = utils.Info{
					Comment: accountInfo.Comment + " - " + roleInfo.Comment,
				}
			}
		}
	}

	return result, nil
}

func GetRootArn(account string) string {
	return fmt.Sprintf("arn:aws:iam::%s:root", account)
}

type roleData struct {
	AccountId string
	Region    string
}

// GetArn returns a list of ARNs based on the given template, account, and region
//
// Example:
//
//	cdk-hnb659fds-deploy-role-{{AccountId}}-{{Region}}" -> [
//			"arn:aws:iam::123456789012:role/cdk-hnb659fds-deploy-role-123456789012-us-west-2"
//	]
func GetArn(role string, account string, region string) (string, error) {
	tmpl, err := template.New(role).Parse(role)
	if err != nil {
		return "", err
	}

	data := roleData{
		AccountId: account,
		Region:    region,
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	return fmt.Sprintf("arn:aws:iam::%s:role/%s", account, buf.String()), err
}
