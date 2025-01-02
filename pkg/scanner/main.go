package scanner

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
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/ryanjarv/roles/pkg/utils"
	"iter"
	"strings"
	"sync"
)

type NewScannerInput struct {
	Config      aws.Config
	Concurrency int
	Name        string
}

func NewScanner(input *NewScannerInput) (*Scanner, error) {
	identity, err := sts.NewFromConfig(input.Config).GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, nil
	}

	scanner := &Scanner{
		Name:            input.Name,
		s3:              s3.NewFromConfig(input.Config),
		s3control:       s3control.NewFromConfig(input.Config),
		AccountId:       *identity.Account,
		Region:          "us-east-1",
		AccessPointName: "role-" + *identity.Account,
		BucketName:      "role-fh9283f-bucket-" + *identity.Account,
		concurrent:      make(chan int, input.Concurrency),
	}
	if input.Config.Region != "" {
		scanner.Region = input.Config.Region
	}

	if err := scanner.Setup(context.Background()); err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}

	return scanner, nil
}

type Scanner struct {
	s3              *s3.Client
	s3control       *s3control.Client
	AccountId       string
	Region          string
	BucketName      string
	AccessPointName string
	concurrent      chan int
	Name            string
}

func (s *Scanner) Setup(ctx context.Context) error {
	if _, err := s.s3.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: &s.BucketName,
		CreateBucketConfiguration: &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(s.Region),
		},
	}); err != nil {
		if oe, ok := err.(smithy.APIError); ok && oe.ErrorCode() != "BucketAlreadyOwnedByYou" {
			return fmt.Errorf("setup bucket: %w", err)
		}
	}
	return nil
}

// ScanArns scans the given ARN for access points
func (s *Scanner) ScanArns(ctx *utils.Context, arns ...string) iter.Seq2[string, bool] {
	done := false
	wg := &sync.WaitGroup{}
	m := &sync.Mutex{}

	return func(yield func(string, bool) bool) {
		for _, arn := range arns {
			if done {
				return
			}

			wg.Add(1)
			s.concurrent <- 1

			go func(arn string) {
				defer func() {
					<-s.concurrent
					wg.Done()
				}()

				exists, err := s.ScanArn(ctx, arn)
				m.Lock()
				if err != nil {
					ctx.Error.Fatalf("scanning %s: %s", arn, err)
				}

				if !done {
					if !yield(arn, exists) {
						done = true
					}
				}
				m.Unlock()
			}(arn) // Pass arn as an argument
		}

		wg.Wait()
	}
}

func (s *Scanner) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	name := s.AccessPointName + "-" + utils.RandStringRunes(8)
	accesspointArn, err := SetupAccessPoint(ctx, s.s3control, name, s.AccountId, s.BucketName)
	if err != nil {
		return false, err
	}

	defer func() {
		if err := DeleteAccessPoint(ctx, *s.s3control, name, s.AccountId); err != nil {
			ctx.Error.Printf("deleting accesspoint: %s", err)
		}
	}()

	policy, err := json.Marshal(utils.PolicyDocument{
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
	})
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
