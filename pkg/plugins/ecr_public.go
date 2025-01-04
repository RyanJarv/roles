package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic/types"
	"github.com/ryanjarv/roles/pkg/utils"
)

// NewECRPublicInput holds the parameters necessary to create new ECR Public plugins.
type NewECRPublicInput struct {
	AccountId string
}

// KnownECRPublicRegions is a list (or map) of regions where ECR Public endpoints actually exist.
// Realistically, ECR Public is often regionless (us-east-1-like endpoint). You could keep it
// to just "us-east-1" or add others if AWS adds them in the future.
var KnownECRPublicRegions = map[string]bool{
	"us-east-1": true,
}

// NewECRPublicRepositories creates a new ECR Public plugin for each region/thread.
func NewECRPublicRepositories(cfgs map[string]aws.Config, concurrency int, input NewECRPublicInput) []Plugin {
	var results []Plugin

	// ECR public only supports us-east-1, there is an us-west-2 endpoint, but it doesn't support the CreateRepository
	// and SetRepositoryPolicy operations.
	region := "us-east-1"
	ecrPublicClient := ecrpublic.NewFromConfig(cfgs[region])

	for i := 0; i < concurrency; i++ {
		repositoryName := fmt.Sprintf("role-fh9283f-ecr-public-%s-%s-%d", region, input.AccountId, i)
		// Construct the ARN deterministically
		results = append(results, &ECRPublicRepository{
			NewECRPublicInput: input,
			thread:            i,
			region:            region,
			repositoryName:    repositoryName,
			repositoryArn:     fmt.Sprintf("arn:aws:ecr-public::%s:repository/%s", input.AccountId, repositoryName),
			client:            ecrPublicClient,
		})
	}

	return results
}

// ECRPublicRepository implements the Plugin interface for ECR Public.
type ECRPublicRepository struct {
	NewECRPublicInput

	thread         int
	region         string
	repositoryName string
	repositoryArn  string

	client IPublicClient
}

// Name returns a unique name for this plugin instance.
func (r *ECRPublicRepository) Name() string {
	return fmt.Sprintf("ecr-public-%s-%d", r.region, r.thread)
}

// Setup creates the ECR Public repository if it doesn't already exist.
func (r *ECRPublicRepository) Setup(ctx *utils.Context) error {
	// Attempt to create the repository.
	// If the repository already exists from a previous run, handle that error gracefully.
	_, err := r.client.CreateRepository(ctx, &ecrpublic.CreateRepositoryInput{
		RepositoryName: &r.repositoryName,
	})
	if err != nil && !strings.Contains(err.Error(), "RepositoryAlreadyExistsException") {
		return fmt.Errorf("creating repository: %w", err)
	}

	return nil
}

// ScanArn attempts to set a policy referencing the provided principal ARN and returns
// true if the principal is valid/existing, false if not.
func (r *ECRPublicRepository) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	// Generate a trust policy referencing this repository ARN and the target principal
	policyDoc, err := json.Marshal(GenerateECRPublicTrustPolicy(r.AccountId, "ecr-public:DescribeRepositories", arn))
	if err != nil {
		return false, fmt.Errorf("marshalling policy: %w", err)
	}

	// Attempt to set the repository policy
	if _, err := r.client.SetRepositoryPolicy(ctx, &ecrpublic.SetRepositoryPolicyInput{
		RepositoryName: &r.repositoryName,
		PolicyText:     aws.String(string(policyDoc)),
	}); ECRPublicNonExistentPrincipalError(err) {
		// If principal doesn't exist
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("setting repository policy: %w", err)
	}

	// If no error, the principal ARN is valid
	return true, nil
}

// CleanUp deletes the ECR Public repository that was created in NewECRPublicRepositories.
func (r *ECRPublicRepository) CleanUp(ctx *utils.Context) error {
	_, err := r.client.DeleteRepository(ctx, &ecrpublic.DeleteRepositoryInput{
		RepositoryName: &r.repositoryName,
	})
	if err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}

	return nil
}

// ECRPublicNonExistentPrincipalError checks if the error indicates a non-existent principal.
func ECRPublicNonExistentPrincipalError(err error) bool {
	var paramErr *types.InvalidParameterException
	return errors.As(err, &paramErr) && strings.Contains(paramErr.ErrorMessage(), "Invalid repository policy provided")
}

// GenerateECRPublicTrustPolicy generates a policy document for ECR Public.
// ECR public doesn't support resource ARNs, and we need to ensure the current account still has access to it.
func GenerateECRPublicTrustPolicy(callerAccountId, action, principalArn string) utils.PolicyDocument {
	return utils.PolicyDocument{
		Version: "2012-10-17",
		Statement: []utils.PolicyStatement{
			{
				Sid:    "us",
				Effect: "Allow",
				Action: "ecr-public:*",
				Principal: utils.PolicyPrincipal{
					AWS: fmt.Sprintf("arn:aws:iam::%s:root", callerAccountId),
				},
			},
			{
				Sid:    "testrole",
				Effect: "Deny",
				Action: action,
				Principal: utils.PolicyPrincipal{
					AWS: principalArn,
				},
			},
		},
	}
}
