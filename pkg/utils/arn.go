package utils

import "fmt"

func GetRootArn(account string) string {
	return fmt.Sprintf("arn:aws:iam::%s:root", account)
}
