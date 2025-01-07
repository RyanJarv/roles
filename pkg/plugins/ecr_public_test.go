package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic/types"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockECRPublicClient implements the methods used by ECRPublicRepository via the ecrpublic.Client.
type mockECRPublicClient struct {
	// Track calls for assertions.
	CreateRepoCalls          int
	SetRepositoryPolicyCalls int
	DeleteRepositoryCalls    int

	// Control whether calls return an error.
	CreateRepoError          error
	SetRepositoryPolicyError error
	DeleteRepositoryError    error
}

// CreateRepository mock.
func (m *mockECRPublicClient) CreateRepository(
	_ context.Context,
	_ *ecrpublic.CreateRepositoryInput,
	_ ...func(*ecrpublic.Options),
) (*ecrpublic.CreateRepositoryOutput, error) {
	m.CreateRepoCalls++
	return &ecrpublic.CreateRepositoryOutput{}, m.CreateRepoError
}

// SetRepositoryPolicy mock.
func (m *mockECRPublicClient) SetRepositoryPolicy(
	_ context.Context,
	_ *ecrpublic.SetRepositoryPolicyInput,
	_ ...func(*ecrpublic.Options),
) (*ecrpublic.SetRepositoryPolicyOutput, error) {
	m.SetRepositoryPolicyCalls++
	return &ecrpublic.SetRepositoryPolicyOutput{}, m.SetRepositoryPolicyError
}

// DeleteRepository mock.
func (m *mockECRPublicClient) DeleteRepository(
	_ context.Context,
	_ *ecrpublic.DeleteRepositoryInput,
	_ ...func(*ecrpublic.Options),
) (*ecrpublic.DeleteRepositoryOutput, error) {
	m.DeleteRepositoryCalls++
	return &ecrpublic.DeleteRepositoryOutput{}, m.DeleteRepositoryError
}

// TestNewECRPublicRepositories tests the creation of plugins, skipping of unsupported regions,
// concurrency, and so on.
func TestNewECRPublicRepositories(t *testing.T) {
	// Provide two regions: one valid, one invalid.
	cfgs := map[string]utils.ThreadConfig{
		"us-east-1": {
			AccountId: "111111111111",
			Region:    "us-east-1",
		},
		"ap-south-2": {
			AccountId: "111111111111",
			Region:    "ap-south-2",
		},
	}

	concurrency := 2

	// Create the plugins
	plugins := NewECRPublicRepositories(cfgs, concurrency)

	// Expect "ap-south-2" to be skipped; only "us-east-1" with concurrency=2 => 2 plugins.
	require.Len(t, plugins, 2, "expected two plugins for us-east-1")

	// Check plugin names
	// ecr-public-us-east-1-<thread>
	expectedNames := []string{
		"ecr-public-us-east-1-0",
		"ecr-public-us-east-1-1",
	}
	for i, p := range plugins {
		assert.Equal(t, expectedNames[i], p.Name())
	}
}

// TestECRPublicSetup tests that Setup creates repositories (or gracefully
// handles them existing).
func TestECRPublicSetup(t *testing.T) {
	mockClient := &mockECRPublicClient{}
	r := &ECRPublicRepository{
		thread:         0,
		repositoryName: "role-fh9283f-ecr-public-us-east-1-123456789012-0",
		repositoryArn:  "arn:aws:ecr-public::123456789012:repository/role-fh9283f-ecr-public-us-east-1-123456789012-0",
		client:         mockClient,
	}

	ctx := &utils.Context{} // Mock or real context

	// 1) Test successful repository creation
	err := r.Setup(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.CreateRepoCalls)

	// 2) Test "RepositoryAlreadyExistsException" scenario
	//    Suppose the repository is already there.
	mockClient.CreateRepoError = errors.New("RepositoryAlreadyExistsException")
	err = r.Setup(ctx)
	require.NoError(t, err, "RepositoryAlreadyExistsException should be handled gracefully")
	assert.Equal(t, 2, mockClient.CreateRepoCalls)
}

// TestECRPublicScanArn tests that ScanArn sets the repository policy and
// interprets error conditions (e.g. principal does not exist).
func TestECRPublicScanArn(t *testing.T) {
	mockClient := &mockECRPublicClient{}
	r := &ECRPublicRepository{
		thread:         0,
		repositoryName: "role-fh9283f-ecr-public-us-east-1-123456789012-0",
		repositoryArn:  "arn:aws:ecr-public::123456789012:repository/role-fh9283f-ecr-public-us-east-1-123456789012-0",
		client:         mockClient,
	}

	ctx := &utils.Context{}
	principalARN := "arn:aws:iam::123456789012:role/SomeTestRole"

	// 1) Success scenario
	exists, err := r.ScanArn(ctx, principalARN)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, 1, mockClient.SetRepositoryPolicyCalls)

	// 2) Principal doesn't exist
	//    We simulate that by returning an InvalidParameterException containing "PrincipalNotFound".
	mockClient.SetRepositoryPolicyError = &types.InvalidParameterException{
		ErrorCodeOverride: aws.String("InvalidParameterException"),
		Message:           aws.String("Invalid parameter at 'PolicyText' failed to satisfy constraint: 'Invalid repository policy provided'"),
	}
	exists, err = r.ScanArn(ctx, principalARN)
	require.NoError(t, err, "non-existent principal should not be a hard error")
	assert.False(t, exists)
	assert.Equal(t, 2, mockClient.SetRepositoryPolicyCalls)

	// 3) Some other error
	mockClient.SetRepositoryPolicyError = errors.New("internal server error")
	exists, err = r.ScanArn(ctx, principalARN)
	require.Error(t, err)
	require.False(t, exists)
	assert.Equal(t, 3, mockClient.SetRepositoryPolicyCalls)
}

// TestECRPublicCleanUp tests that CleanUp deletes the repository correctly.
func TestECRPublicCleanUp(t *testing.T) {
	mockClient := &mockECRPublicClient{}
	r := &ECRPublicRepository{
		thread:         0,
		repositoryName: "role-fh9283f-ecr-public-us-east-1-123456789012-0",
		repositoryArn:  "arn:aws:ecr-public::123456789012:repository/role-fh9283f-ecr-public-us-east-1-123456789012-0",
		client:         mockClient,
	}

	ctx := &utils.Context{}

	// 1) Successful delete
	err := r.CleanUp(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.DeleteRepositoryCalls)

	// 2) Deletion error scenario
	mockClient.DeleteRepositoryError = errors.New("delete failed")
	err = r.CleanUp(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete failed")
	assert.Equal(t, 2, mockClient.DeleteRepositoryCalls)
}

// Example: Checking behavior when region is not in KnownECRPublicRegions
// If you have "ap-south-2" not in the map, it should skip it entirely.
func TestNewECRPublicRepositories_UnsupportedRegion(t *testing.T) {
	oldKnownRegions := KnownECRPublicRegions
	// Temporarily override the known region map to confirm skip behavior
	KnownECRPublicRegions = map[string]bool{
		"us-east-1": true,
	}
	defer func() { KnownECRPublicRegions = oldKnownRegions }()

	cfgs := map[string]utils.ThreadConfig{
		"us-east-1":  {},
		"ap-south-2": {},
	}
	concurrency := 1

	// Capture stdout or logs if you want to test the warning message, or just rely on plugin count.
	plugs := NewECRPublicRepositories(cfgs, concurrency)
	require.Len(t, plugs, 1, "should only create plugin for us-east-1")
	assert.Equal(t, "ecr-public-us-east-1-0", plugs[0].Name())
}
