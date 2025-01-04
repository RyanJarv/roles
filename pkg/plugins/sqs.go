package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/ryanjarv/roles/pkg/utils"
	"strings"
)

type NewSQSInput struct {
	AccountId string
}

// NewSQSQueues constructs plugin instances (SQSQueue) for each region/thread.
func NewSQSQueues(cfgs map[string]aws.Config, concurrency int, input NewSQSInput) []Plugin {
	var results []Plugin

	for region, cfg := range cfgs {
		sqsClient := sqs.NewFromConfig(cfg)

		for i := 0; i < concurrency; i++ {
			queueName := fmt.Sprintf("role-fh9283f-sqs-%s-%s-%d", region, input.AccountId, i)

			results = append(results, &SQSQueue{
				NewSQSInput: input,
				thread:      i,
				region:      region,
				queueName:   queueName,
				sqsClient:   sqsClient,
				queueUrl:    fmt.Sprintf("https://sqs.%s.amazonaws.com/%s/%s", region, input.AccountId, queueName),
				queueArn:    fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, input.AccountId, queueName),
			})
		}
	}

	return results
}

type SQSQueue struct {
	NewSQSInput

	thread    int
	region    string
	queueName string
	queueArn  string
	queueUrl  string

	sqsClient *sqs.Client
}

func (s *SQSQueue) Name() string {
	return fmt.Sprintf("sqs-%s-%d", s.region, s.thread)
}

// Setup creates the queue and retrieves its URL and ARN.
// This method is now responsible for actually provisioning the SQS resource.
func (s *SQSQueue) Setup(ctx *utils.Context) error {
	if _, err := s.sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: &s.queueName,
	}); err != nil {
		return fmt.Errorf("create queue: %w", err)
	}

	return nil
}

// ScanArn attempts to update the SQS queue policy referencing the provided ARN.
// If the role ARN doesn't exist, SQS will typically return an error referencing "invalid principal" or "PrincipalNotFound".
func (s *SQSQueue) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	// Build a trust policy referencing this queue ARN as the Resource, and the target role ARN as the Principal.
	policyDoc, err := json.Marshal(utils.GenerateTrustPolicy(s.queueArn, "SQS:SendMessage", arn))
	if err != nil {
		return false, fmt.Errorf("marshalling policy: %w", err)
	}

	if _, err = s.sqsClient.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: &s.queueUrl,
		Attributes: map[string]string{
			"Policy": string(policyDoc),
		},
	}); SQSNonExistentPrincipalError(err) {
		// Means the role doesn't exist
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("setting queue policy: %w", err)
	}

	// No error => role principal is valid
	return true, nil
}

// CleanUp deletes the SQS queue that was created in Setup().
func (s *SQSQueue) CleanUp(ctx *utils.Context) error {
	if s.queueUrl == "" {
		return nil
	}

	_, err := s.sqsClient.DeleteQueue(ctx, &sqs.DeleteQueueInput{
		QueueUrl: &s.queueUrl,
	})
	if err != nil {
		return fmt.Errorf("deleting queue: %w", err)
	}

	return nil
}

// SQSNonExistentPrincipalError checks if an error indicates a non-existent or invalid principal.
func SQSNonExistentPrincipalError(err error) bool {
	var invalidAttr *types.InvalidAttributeValue
	if errors.As(err, &invalidAttr) &&
		invalidAttr.ErrorCode() == "InvalidAttributeValue" &&
		strings.Contains(invalidAttr.ErrorMessage(), "Invalid value for the parameter Policy") {
		return true
	}

	return false
}