package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSNSClient implements just the methods used by SNSTopic via sns.Client.
type mockSNSClient struct {
	CreateTopicCount        int
	SetTopicAttributesCount int
	DeleteTopicCount        int

	// Control errors for each method to simulate real scenarios.
	CreateTopicError        error
	SetTopicAttributesError error
	DeleteTopicError        error
}

// CreateTopic mock
func (m *mockSNSClient) CreateTopic(
	_ context.Context,
	_ *sns.CreateTopicInput,
	_ ...func(*sns.Options),
) (*sns.CreateTopicOutput, error) {
	m.CreateTopicCount++
	return &sns.CreateTopicOutput{}, m.CreateTopicError
}

// SetTopicAttributes mock
func (m *mockSNSClient) SetTopicAttributes(
	_ context.Context,
	_ *sns.SetTopicAttributesInput,
	_ ...func(*sns.Options),
) (*sns.SetTopicAttributesOutput, error) {
	m.SetTopicAttributesCount++
	return &sns.SetTopicAttributesOutput{}, m.SetTopicAttributesError
}

// DeleteTopic mock
func (m *mockSNSClient) DeleteTopic(
	_ context.Context,
	_ *sns.DeleteTopicInput,
	_ ...func(*sns.Options),
) (*sns.DeleteTopicOutput, error) {
	m.DeleteTopicCount++
	return &sns.DeleteTopicOutput{}, m.DeleteTopicError
}

// TestNewSNSTopics tests the plugin creation logic (for each region/thread).
func TestNewSNSTopics(t *testing.T) {
	// Suppose we have two regions. For concurrency=2, that means we expect 2 threads per region -> 4 total plugins.
	cfgs := map[string]utils.ThreadConfig{
		"us-east-1": {},
		"us-west-2": {},
	}
	concurrency := 2

	plugins := NewSNSTopics(cfgs, concurrency)

	// We should have 4 plugins in total.
	require.Len(t, plugins, 4)

	// Inspect a few plugin attributes.
	// The first plugin should be region=us-east-1, thread=0
	sns0 := plugins[0].(*SNSTopic)
	assert.Equal(t, "us-east-1", sns0.Region)
	assert.Equal(t, 0, sns0.thread)
	assert.Equal(t, "role-fh9283f-sns-us-east-1-123456789012-0", sns0.topicName)
	assert.Equal(t, "arn:aws:sns:us-east-1:123456789012:role-fh9283f-sns-us-east-1-123456789012-0", sns0.topicArn)

	// The last plugin should be region=us-west-2, thread=1
	sns3 := plugins[3].(*SNSTopic)
	assert.Equal(t, "us-west-2", sns3.Region)
	assert.Equal(t, 1, sns3.thread)
	assert.Equal(t, "role-fh9283f-sns-us-west-2-123456789012-1", sns3.topicName)
}

// TestSNSTopicSetup tests that Setup creates the topic or handles errors.
func TestSNSTopicSetup(t *testing.T) {
	mockClient := &mockSNSClient{}
	topic := &SNSTopic{
		thread:    0,
		topicName: "role-fh9283f-sns-us-east-1-123456789012-0",
		topicArn:  "arn:aws:sns:us-east-1:123456789012:role-fh9283f-sns-us-east-1-123456789012-0",
		snsClient: mockClient,
	}

	ctx := &utils.Context{}

	// 1) Success scenario: no error from CreateTopic
	err := topic.Setup(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.CreateTopicCount)

	// 2) Error scenario
	mockClient.CreateTopicError = errors.New("some creation error")
	err = topic.Setup(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating topic")
	assert.Equal(t, 2, mockClient.CreateTopicCount)
}

// TestSNSTopicScanArn tests the ScanArn method behavior.
func TestSNSTopicScanArn(t *testing.T) {
	mockClient := &mockSNSClient{}
	topic := &SNSTopic{
		thread:    0,
		topicName: "role-fh9283f-sns-us-east-1-123456789012-0",
		topicArn:  "arn:aws:sns:us-east-1:123456789012:role-fh9283f-sns-us-east-1-123456789012-0",
		snsClient: mockClient,
	}

	ctx := &utils.Context{}
	principalArn := "arn:aws:iam::123456789012:role/SomeTestRole"

	// 1) Success scenario
	exists, err := topic.ScanArn(ctx, principalArn)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, 1, mockClient.SetTopicAttributesCount)

	// 2) Principal doesn't exist -> "PrincipalNotFound" or "InvalidArn"
	mockClient.SetTopicAttributesError = &types.InvalidParameterException{
		ErrorCodeOverride: aws.String("InvalidParameterException"),
		Message:           aws.String("PrincipalNotFound"),
	}
	exists, err = topic.ScanArn(ctx, principalArn)
	require.NoError(t, err, "non-existent principal should not raise a fatal error")
	assert.False(t, exists)
	assert.Equal(t, 2, mockClient.SetTopicAttributesCount)

	// 3) Some other error
	mockClient.SetTopicAttributesError = errors.New("some unknown error")
	exists, err = topic.ScanArn(ctx, principalArn)
	require.Error(t, err)
	assert.False(t, exists)
	assert.Contains(t, err.Error(), "setting topic policy")
	assert.Equal(t, 3, mockClient.SetTopicAttributesCount)
}

// TestSNSTopicCleanUp tests that CleanUp deletes the SNS topic.
func TestSNSTopicCleanUp(t *testing.T) {
	mockClient := &mockSNSClient{}
	topic := &SNSTopic{
		thread:    0,
		topicName: "role-fh9283f-sns-us-east-1-123456789012-0",
		topicArn:  "arn:aws:sns:us-east-1:123456789012:role-fh9283f-sns-us-east-1-123456789012-0",
		snsClient: mockClient,
	}

	ctx := &utils.Context{}

	// 1) Successful delete
	err := topic.CleanUp(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, mockClient.DeleteTopicCount)

	// 2) Error deleting
	mockClient.DeleteTopicError = errors.New("delete failed")
	err = topic.CleanUp(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleting topic")
	assert.Equal(t, 2, mockClient.DeleteTopicCount)
}

// Example test that verifies the Name() method produces the expected string.
func TestSNSTopicName(t *testing.T) {
	topic := &SNSTopic{
		thread: 3,
	}
	assert.Equal(t, "sns-us-west-2-3", topic.Name())
}
