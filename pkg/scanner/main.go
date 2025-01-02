package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/s3control"
	s3controlTypes "github.com/aws/aws-sdk-go-v2/service/s3control/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"iter"
	"strings"
	"sync"
)

type NewScannerInput struct {
	Config      aws.Config
	Concurrency int
	Storage     *Storage
	Plugins     []plugins.Plugin
	Force       bool
}

func NewScanner(ctx *utils.Context, input *NewScannerInput) (*Scanner, error) {
	identity, err := sts.NewFromConfig(input.Config).GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, nil
	}

	region := "us-east-1"
	if input.Config.Region != "" {
		region = input.Config.Region
	}

	scanner := &Scanner{
		s3:              s3.NewFromConfig(input.Config),
		s3control:       s3control.NewFromConfig(input.Config),
		AccountId:       *identity.Account,
		Region:          region,
		AccessPointName: fmt.Sprintf("role-%s-%s", region, *identity.Account),
		BucketName:      fmt.Sprintf("role-fh9283f-bucket-%s-%s", input.Config.Region, *identity.Account),
		concurrent:      make(chan int, input.Concurrency),
		storage:         input.Storage,
		force:           input.Force,
	}
	if input.Config.Region != "" {
		scanner.Region = input.Config.Region
	}

	if err := scanner.Setup(ctx); err != nil {
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
	storage         *Storage
	force           bool
}

func (s *Scanner) Setup(ctx *utils.Context) error {
	var conf *s3Types.CreateBucketConfiguration

	// This shit is fucking wild, us-east-1 isn't a valid region here. Who do these people even think they are?
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

// ScanArns scans the given ARN for access points
func (s *Scanner) ScanArns(ctx *utils.Context, principalArns []string) iter.Seq2[string, bool] {
	return func(yield func(string, bool) bool) {
		rootArnMap := RootArnMap(ctx, principalArns)

		var rootArnsToScan []string
		var allAccountArns []string

		if s.force {
			rootArnsToScan = lo.Keys(rootArnMap)
		} else {
			for rootArn, accountArns := range rootArnMap {
				status, _, err := s.storage.GetStatus(rootArn)
				if err != nil {
					ctx.Error.Fatalf("GetStatus: %s", err)
				}
				if status == PrincipalDoesNotExist {
					continue
				} else if status == PrincipalExists {
					allAccountArns = append(allAccountArns, accountArns...)
				} else if status == PrincipalUnknown {
					rootArnsToScan = append(rootArnsToScan, rootArn)
				} else {
					ctx.Error.Fatalf("unknown status: %d", status)
				}
			}
		}

		for principalArn, exists := range s.scanArns(ctx, rootArnsToScan) {
			if !yield(principalArn, exists) {
				return
			}
			if exists {
				allAccountArns = append(allAccountArns, rootArnMap[principalArn]...)
			}
		}

		var accountArnsToScan []string
		if s.force {
			accountArnsToScan = allAccountArns
		} else {
			for _, principalArn := range allAccountArns {
				status, _, err := s.storage.GetStatus(principalArn)
				if err != nil {
					ctx.Error.Fatalf("GetStatus: %s", err)
				}
				if status == PrincipalUnknown {
					accountArnsToScan = append(accountArnsToScan, principalArn)
				} else {
					if !yield(principalArn, status == PrincipalExists) {
						return
					}
				}
			}
		}

		for principalArn, exists := range s.scanArns(ctx, allAccountArns) {
			yield(principalArn, exists)
		}
	}
}

//func (s *Scanner) scanArns(ctx *utils.Context, principalArns []string) iter.Seq2[string, bool] {
//	done := false
//	wg := &sync.WaitGroup{}
//
//	return func(yield func(string, bool) bool) {
//		for i, principalArn := range principalArns {
//			//if done {
//			//	return
//			//}
//
//			wg.Add(1)
//			s.concurrent <- 1
//
//			go func(principalArn string) {
//				defer func() {
//					<-s.concurrent
//					wg.Done()
//				}()
//
//				exists, err := s.ScanArn(ctx, principalArn, i, nil)
//				if err != nil {
//					ctx.Error.Fatalf("scanning %s: %s", principalArn, err)
//				}
//
//				if err := s.storage.Set(principalArn, utils.Info{Exists: true}); err != nil {
//					ctx.Error.Fatalf("setting status: %s", err)
//				}
//
//				if !done {
//					if !yield(principalArn, exists) {
//						done = true
//					}
//				}
//			}(principalArn)
//		}
//
//		wg.Wait()
//	}
//}

func (s *Scanner) scanArns(ctx *utils.Context, principalArns []string) iter.Seq2[string, bool] {
	wg := &sync.WaitGroup{}

	return func(yield func(string, bool) bool) {
		for i, principalArn := range principalArns {

			wg.Add(1)
			s.concurrent <- 1

			if !s.ScanArn(ctx, principalArn, i, func(exists bool, err error) bool {
				<-s.concurrent
				wg.Done()

				if err := s.storage.Set(principalArn, utils.Info{Exists: true}); err != nil {
					ctx.Error.Fatalf("setting status: %s", err)
				}

				if !yield(principalArn, exists) {
					return false
				}

				return true
			}) {
				return
			}
		}

		wg.Wait()
	}
}

func (s *Scanner) ScanArn(ctx *utils.Context, arn string, i int, cb func(exists bool, err error) bool) {
	name := fmt.Sprintf("%s-%s-%d-%s", s.AccessPointName, s.Region, i, utils.RandStringRunes(12))
	accesspointArn, err := SetupAccessPoint(ctx, s.s3control, name, s.AccountId, s.BucketName)
	if err != nil {
		return cb(false, err)
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
		cb(false, fmt.Errorf("marshalling policy: %w", err))
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
				cb(false, nil)
			}
		}
		cb(false, fmt.Errorf("updating policy: %w", err))
	}

	ctx.Debug.Printf("found: %s", arn)
	cb(true, nil)
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

func RootArnMap(ctx *utils.Context, principalArns []string) map[string][]string {
	result := map[string][]string{}

	for _, principalArn := range principalArns {
		parsed, err := arn.Parse(principalArn)
		if err != nil {
			ctx.Error.Fatalf("parsing arn: %s", err)
		}

		rootArn := utils.GetRootArn(parsed.AccountID)
		if _, ok := result[rootArn]; !ok {
			result[rootArn] = []string{}
		}

		if parsed.Resource != "root" {
			result[rootArn] = append(result[parsed.AccountID], principalArn)
		}
	}

	return result
}
