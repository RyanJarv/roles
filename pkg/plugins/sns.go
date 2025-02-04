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

// NewSNSTopics creates a new SNS plugin for each region/thread.
func NewSNSTopics(cfgs map[string]utils.ThreadConfig, concurrency int) []Plugin {
	var results []Plugin

	for _, cfg := range cfgs {
		// Create a single sns.Client per region
		snsClient := sns.NewFromConfig(cfg.Config)

		for i := 0; i < concurrency; i++ {
			topicName := fmt.Sprintf("role-fh9283f-sns-%s-%s-%d", cfg.Region, cfg.AccountId, i)

			results = append(results, &SNSTopic{
				ThreadConfig: cfg,
				thread:       i,
				topicName:    topicName,
				topicArn:     fmt.Sprintf("arn:aws:sns:%s:%s:%s", cfg.Region, cfg.AccountId, topicName),
				snsClient:    snsClient,
			})
		}
	}

	return results
}

type SNSTopic struct {
	utils.ThreadConfig
	thread    int
	topicName string
	topicArn  string

	snsClient ISNSClient
}

func (t *SNSTopic) Name() string {
	return fmt.Sprintf("sns-%s-%s-%d", t.AccountId, t.Region, t.thread)
}

// Setup creates the SNS topic if it doesn't already exist.
func (t *SNSTopic) Setup(ctx *utils.Context) error {
	ctx.Debug.Printf("creating SNS topic %s", t.topicName)
	_, err := t.snsClient.CreateTopic(ctx, &sns.CreateTopicInput{
		Name: &t.topicName,
	})
	if err != nil {
		return fmt.Errorf("creating topic: %w", err)
	}

	return nil
}

// ScanArn updates the SNS topic policy referencing the provided ARN and returns true if the principal is valid.
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
	_, err := t.snsClient.DeleteTopic(ctx, &sns.DeleteTopicInput{
		TopicArn: &t.topicArn,
	})
	if err != nil {
		return fmt.Errorf("deleting topic: %w", err)
	}

	return nil
}

// SNSNonExistentPrincipalError checks if the error is due to a non-existent principal.
func SNSNonExistentPrincipalError(err error) bool {
	var paramErr *types.InvalidParameterException
	return errors.As(err, &paramErr) && (strings.Contains(paramErr.ErrorMessage(), "PrincipalNotFound") || strings.Contains(paramErr.ErrorMessage(), "InvalidArn"))
}
