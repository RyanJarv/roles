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

const Concurrency = 5

type NewAccessPointInput struct {
	AccountId string
}

// NewAccessPoints creates a new access point plugin for each region.
func NewAccessPoints(cfgs map[string]aws.Config, input NewAccessPointInput) []Plugin {
	results := []Plugin{}

	for region, cfg := range cfgs {
		for i := 0; i < Concurrency; i++ {
			results = append(results, &AccessPoint{
				NewAccessPointInput: input,
				thread:              i,
				// Make sure each thread has its own unique bucket and access point name.
				accessPointName: fmt.Sprintf("role-%s-%d", region, i),
				bucketName:      fmt.Sprintf("role-fh9283f-bucket-%s-%s-%d", cfg.Region, input.AccountId, i),
				region:          region,
				s3:              s3.NewFromConfig(cfg),
				s3control:       s3control.NewFromConfig(cfg),
			})
		}
	}

	return results
}

type AccessPoint struct {
	NewAccessPointInput
	thread          int
	s3              *s3.Client
	s3control       *s3control.Client
	region          string
	accessPointName string
	bucketName      string
	accesspointArn  string
}

func (s *AccessPoint) Name() string { return fmt.Sprintf("access-point-%s-%d", s.region, s.thread) }

// Setup creates the bucket for this region if it doesn't exist.
func (s *AccessPoint) Setup(ctx *utils.Context) error {
	var conf *s3Types.CreateBucketConfiguration

	// This shit is fucking wild, us-east-1 isn't a valid region here. Who do these people even think they are?
	if s.region != "us-east-1" {
		conf = &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(s.region),
		}
	}

	if _, err := s.s3.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                    &s.bucketName,
		CreateBucketConfiguration: conf,
	}); err != nil {
		var yourBucketErr *s3Types.BucketAlreadyOwnedByYou
		if ok := errors.As(err, &yourBucketErr); ok {
			ctx.Debug.Printf("bucket already owned by us: %s", s.bucketName)
		} else {
			return fmt.Errorf("create bucket: %w", err)
		}
	}

	var err error
	s.accesspointArn, err = SetupAccessPoint(ctx, s.s3control, s.accessPointName, s.AccountId, s.bucketName)
	if err != nil {
		return fmt.Errorf("setup access point: %w", err)
	}

	return nil
}

func (s *AccessPoint) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	policy, err := json.Marshal(GenerateTrustPolicy(s.accesspointArn, arn))
	if err != nil {
		return false, fmt.Errorf("marshalling policy: %w", err)
	}

	_, err = s.s3control.PutAccessPointPolicy(ctx, &s3control.PutAccessPointPolicyInput{
		AccountId: &s.AccountId,
		Name:      &s.accessPointName,
		Policy:    aws.String(string(policy)),
	})
	if err != nil {
		oe := &smithy.GenericAPIError{}
		if ok := errors.As(err, &oe); ok && oe.ErrorCode() == "MalformedPolicy" {
			if strings.Contains(strings.ToLower(oe.ErrorMessage()), "invalid principal") {
				return false, nil
			}
		}
		return false, fmt.Errorf("updating policy: %w", err)
	}

	return true, nil
}

func (s *AccessPoint) CleanUp(ctx *utils.Context) error {
	points, err := s.s3control.ListAccessPoints(ctx, &s3control.ListAccessPointsInput{
		AccountId: &s.AccountId,
		Bucket:    &s.bucketName,
	})
	if err != nil {
		return fmt.Errorf("listing access points: %w", err)
	}

	for _, point := range points.AccessPointList {
		ctx.Debug.Printf("deleting up accesspoint %s", *point.Name)
		if err := DeleteAccessPoint(ctx, *s.s3control, *point.Name, s.AccountId); err != nil {
			ctx.Error.Printf("deleting accesspoint: %s", err)
		}
	}

	return nil
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
