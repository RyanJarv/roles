package arn

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetArn(t *testing.T) {
	got, err := GetArn("role/cdk-hnb659fds-deploy-role-{{.AccountId}}-{{.Region}}", "123456789012", "us-west-2")
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam::123456789012:role/cdk-hnb659fds-deploy-role-123456789012-us-west-2", got)

	got, err = GetArn("user/alice-{{.Region}}", "123456789012", "us-east-1")
	require.NoError(t, err)
	assert.Equal(t, "arn:aws:iam::123456789012:user/alice-us-east-1", got)
}

func TestGetArns_MergesRolesAndPrincipals(t *testing.T) {
	ctx := utils.NewContext(context.Background())
	dir := t.TempDir()
	rolesPath := filepath.Join(dir, "roles.list")
	principalsPath := filepath.Join(dir, "principals.list")

	require.NoError(t, os.WriteFile(rolesPath, []byte("Admin # role comment\n"), 0o600))
	require.NoError(t, os.WriteFile(principalsPath, []byte("user/alice-{{.Region}} # user comment\n"), 0o600))

	got, err := GetArns(ctx, &GetArnsInput{
		AccountsStr:    "123456789012",
		RolePaths:      []string{rolesPath},
		PrincipalPaths: []string{principalsPath},
		Regions: map[string]utils.Info{
			"us-east-1": {},
		},
	})
	require.NoError(t, err)

	assert.Contains(t, got, "arn:aws:iam::123456789012:root")
	assert.Equal(t, " -  role comment", got["arn:aws:iam::123456789012:role/Admin"].Comment)
	assert.Equal(t, " -  user comment", got["arn:aws:iam::123456789012:user/alice-us-east-1"].Comment)
}

func TestGetRoleInputs_AddsRolePrefix(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/roles.list"
	require.NoError(t, os.WriteFile(path, []byte("Admin\npath/Operator # comment\n"), 0o600))

	got, err := getRoleInputs([]string{path})
	require.NoError(t, err)
	assert.Equal(t, utils.Info{}, got["role/Admin"])
	assert.Equal(t, " comment", got["role/path/Operator"].Comment)
}

func TestGetPrincipalInputs_RejectsMissingPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "principals.list")
	require.NoError(t, os.WriteFile(path, []byte("Admin\n"), 0o600))

	_, err := getPrincipalInputs([]string{path})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must start with role/ or user/")
}
