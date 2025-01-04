package plugins

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/ryanjarv/roles/pkg/utils"
)

type Plugin interface {
	Name() string
	Setup(ctx *utils.Context) error
	ScanArn(ctx *utils.Context, arn string) (bool, error)
	CleanUp(ctx *utils.Context) error
}

type IECRPublicClient interface {
	CreateRepository(ctx context.Context, params *ecrpublic.CreateRepositoryInput, optFns ...func(*ecrpublic.Options)) (*ecrpublic.CreateRepositoryOutput, error)
	SetRepositoryPolicy(ctx context.Context, params *ecrpublic.SetRepositoryPolicyInput, optFns ...func(*ecrpublic.Options)) (*ecrpublic.SetRepositoryPolicyOutput, error)
	DeleteRepository(ctx context.Context, params *ecrpublic.DeleteRepositoryInput, optFns ...func(*ecrpublic.Options)) (*ecrpublic.DeleteRepositoryOutput, error)
}

type ISNSClient interface {
	CreateTopic(ctx context.Context, params *sns.CreateTopicInput, optFns ...func(*sns.Options)) (*sns.CreateTopicOutput, error)
	SetTopicAttributes(ctx context.Context, params *sns.SetTopicAttributesInput, optFns ...func(*sns.Options)) (*sns.SetTopicAttributesOutput, error)
	DeleteTopic(ctx context.Context, params *sns.DeleteTopicInput, optFns ...func(*sns.Options)) (*sns.DeleteTopicOutput, error)
}
