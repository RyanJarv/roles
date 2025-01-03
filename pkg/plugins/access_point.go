package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/s3control"
	s3controlTypes "github.com/aws/aws-sdk-go-v2/service/s3control/types"
	"github.com/aws/smithy-go"
	"github.com/ryanjarv/roles/pkg/utils"
	"strings"
)

type NewAccessPointInput struct {
	Config    aws.Config
	AccountId string
}

func NewAccessPoint(input NewAccessPointInput) *AccessPoint {
	region := "us-east-1"

	if input.Config.Region != "" {
		region = input.Config.Region
	}

	return &AccessPoint{
		NewAccessPointInput: input,
		AccessPointName:     fmt.Sprintf("role-%s-%s", region, input.AccountId),
		BucketName:          fmt.Sprintf("role-fh9283f-bucket-%s-%s", input.Config.Region, input.AccountId),
		Region:              region,
		s3:                  s3.NewFromConfig(input.Config),
		s3control:           s3control.NewFromConfig(input.Config),
	}
}

type AccessPoint struct {
	NewAccessPointInput
	s3              *s3.Client
	s3control       *s3control.Client
	Region          string
	AccessPointName string
	BucketName      string
}

func (s *AccessPoint) Name() string     { return "access-point" }
func (s *AccessPoint) Concurrency() int { return 5 }

func (s *AccessPoint) Setup(ctx *utils.Context) error {
	var conf *s3Types.CreateBucketConfiguration

	// This shit is fucking wild, us-east-1 isn't a valid Region here. Who do these people even think they are?
	if s.Region != "us-east-1" {
		conf = &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(s.Region),
		}
	}

	if _, err := s.s3.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                    &s.BucketName,
		CreateBucketConfiguration: conf,
	}); err != nil {
		if oe, ok := err.(smithy.APIError); !ok || oe.ErrorCode() != "BucketAlreadyOwnedByYou" {
			return fmt.Errorf("setup bucket: %w", err)
		}
	}
	return nil
}

func (s *AccessPoint) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	name := fmt.Sprintf("%s-%s-%s", s.AccessPointName, s.Region, utils.RandStringRunes(12))
	accesspointArn, err := SetupAccessPoint(ctx, s.s3control, name, s.AccountId, s.BucketName)
	if err != nil {
		return false, err
	}

	defer func() {
		if err := DeleteAccessPoint(ctx, *s.s3control, name, s.AccountId); err != nil {
			ctx.Error.Printf("deleting accesspoint: %s", err)
		}
	}()

	policy, err := json.Marshal(GenerateTrustPolicy(accesspointArn, arn))
	if err != nil {
		return false, fmt.Errorf("marshalling policy: %w", err)
	}

	_, err = s.s3control.PutAccessPointPolicy(ctx, &s3control.PutAccessPointPolicyInput{
		AccountId: &s.AccountId,
		Name:      &name,
		Policy:    aws.String(string(policy)),
	})
	if err != nil {
		oe := &smithy.GenericAPIError{}
		if ok := errors.As(err, &oe); ok && oe.ErrorCode() == "MalformedPolicy" {
			if strings.Contains(strings.ToLower(oe.ErrorMessage()), "invalid principal") {
				ctx.Debug.Printf("not found: %s", arn)
				return false, nil
			}
		}
		return false, fmt.Errorf("updating policy: %w", err)
	}

	ctx.Debug.Printf("found: %s", arn)
	return true, nil
}

func SetupAccessPoint(ctx context.Context, api *s3control.Client, name, account, bucket string) (string, error) {
	accessPoint, err := api.CreateAccessPoint(ctx, &s3control.CreateAccessPointInput{
		Name:            &name,
		AccountId:       &account,
		Bucket:          &bucket,
		BucketAccountId: &account,
		PublicAccessBlockConfiguration: &s3controlTypes.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			RestrictPublicBuckets: aws.Bool(true),
		},
	})
	if err != nil {
		oe := &smithy.GenericAPIError{}
		if errors.As(err, &oe) && oe.ErrorCode() == "AccessPointAlreadyOwnedByYou" {
			point, err := api.GetAccessPoint(ctx, &s3control.GetAccessPointInput{
				Name:      &name,
				AccountId: &account,
			})
			if err != nil {
				return "", fmt.Errorf("get accesspoint: %w", err)
			}

			return *point.AccessPointArn, nil
		}

		return "", fmt.Errorf("setup access point: %w", err)
	}

	return *accessPoint.AccessPointArn, nil
}

func DeleteAccessPoint(ctx context.Context, api s3control.Client, name string, account string) error {
	if _, err := api.DeleteAccessPoint(ctx, &s3control.DeleteAccessPointInput{
		Name:      &name,
		AccountId: &account,
	}); err != nil {
		return fmt.Errorf("teardown: %w", err)
	}

	return nil
}

func GenerateTrustPolicy(accesspointArn string, arn string) utils.PolicyDocument {
	document := utils.PolicyDocument{
		Version: "2012-10-17",
		Statement: []utils.PolicyStatement{
			{
				Sid:      "testrole",
				Effect:   "Deny",
				Action:   "*",
				Resource: accesspointArn,
				Principal: utils.PolicyPrincipal{
					AWS: arn,
				},
			},
		},
	}
	return document
}
