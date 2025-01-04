package plugins

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/service/ecrpublic"
	"github.com/ryanjarv/roles/pkg/utils"
)

type Plugin interface {
	Name() string
	Setup(ctx *utils.Context) error
	ScanArn(ctx *utils.Context, arn string) (bool, error)
	CleanUp(ctx *utils.Context) error
}

type IPublicClient interface {
	CreateRepository(ctx context.Context, params *ecrpublic.CreateRepositoryInput, optFns ...func(*ecrpublic.Options)) (*ecrpublic.CreateRepositoryOutput, error)
	SetRepositoryPolicy(ctx context.Context, params *ecrpublic.SetRepositoryPolicyInput, optFns ...func(*ecrpublic.Options)) (*ecrpublic.SetRepositoryPolicyOutput, error)
	DeleteRepository(ctx context.Context, params *ecrpublic.DeleteRepositoryInput, optFns ...func(*ecrpublic.Options)) (*ecrpublic.DeleteRepositoryOutput, error)
}
