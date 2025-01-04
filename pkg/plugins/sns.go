package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/ryanjarv/roles/pkg/utils"
	"strings"
)

type NewSNSInput struct {
	AccountId string
}

// NewSNSTopics creates each SNS topic upfront and stores its ARN.
// We do *not* rely on Setup() to create anything.
func NewSNSTopics(cfgs map[string]aws.Config, concurrency int, input NewSNSInput) []Plugin {
	var results []Plugin

	for region, cfg := range cfgs {
		// Create a single sns.Client per region
		snsClient := sns.NewFromConfig(cfg)

		for i := 0; i < concurrency; i++ {
			topicName := fmt.Sprintf("role-fh9283f-sns-%s-%s-%d", region, input.AccountId, i)

			results = append(results, &SNSTopic{
				NewSNSInput: input,
				thread:      i,
				region:      region,
				topicName:   topicName,
				topicArn:    fmt.Sprintf("arn:aws:sns:%s:%s:%s", region, input.AccountId, topicName),
				snsClient:   snsClient,
			})
		}
	}

	return results
}

type SNSTopic struct {
	NewSNSInput

	thread    int
	region    string
	topicName string
	topicArn  string

	snsClient *sns.Client
}

func (t *SNSTopic) Name() string {
	return fmt.Sprintf("sns-%s-%d", t.region, t.thread)
}

// Setup is a no-op in this version, since we already created the topic in NewSNSTopics.
func (t *SNSTopic) Setup(ctx *utils.Context) error {
	return nil
}

// ScanArn updates the SNS topic policy referencing the provided ARN.
// If the role ARN doesn't exist, we'll typically see an "InvalidParameter" or
// "AuthorizationError" with "invalid principal."
func (t *SNSTopic) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	// Generate a trust policy referencing the topic ARN and the target role ARN
	policyDoc, err := json.Marshal(utils.GenerateTrustPolicy(t.topicArn, "SNS:GetTopicAttributes", arn))
	if err != nil {
		return false, fmt.Errorf("marshalling policy: %w", err)
	}

	// Attempt to set the topic policy
	if _, err := t.snsClient.SetTopicAttributes(ctx, &sns.SetTopicAttributesInput{
		TopicArn:       &t.topicArn,
		AttributeName:  aws.String("Policy"),
		AttributeValue: aws.String(string(policyDoc)),
	}); SNSNonExistentPrincipalError(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("setting topic policy: %w", err)
	}

	// If no error, the role principal is valid
	return true, nil
}

// CleanUp deletes the SNS topic that was created in NewSNSTopics.
func (t *SNSTopic) CleanUp(ctx *utils.Context) error {
	if t.topicArn == "" {
		// No ARN stored, so we can't delete anything
		return nil
	}

	_, err := t.snsClient.DeleteTopic(ctx, &sns.DeleteTopicInput{
		TopicArn: &t.topicArn,
	})
	if err != nil {
		return fmt.Errorf("deleting topic: %w", err)
	}

	return nil
}

func SNSNonExistentPrincipalError(err error) bool {
	var paramErr *types.InvalidParameterException
	return errors.As(err, &paramErr) && (strings.Contains(paramErr.ErrorMessage(), "PrincipalNotFound") || strings.Contains(paramErr.ErrorMessage(), "InvalidArn"))
}
