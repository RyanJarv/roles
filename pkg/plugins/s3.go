package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ryanjarv/roles/pkg/utils"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type NewS3BucketInput struct {
	AccountId string
}

// NewS3Buckets creates a new S3 plugin for each region/thread.
func NewS3Buckets(cfgs map[string]utils.ThreadConfig, concurrency int) []Plugin {
	results := []Plugin{}

	for _, cfg := range cfgs {
		for i := 0; i < concurrency; i++ {
			results = append(results, &S3Bucket{
				ThreadConfig: cfg,
				thread:       i,
				bucketName:   fmt.Sprintf("role-fh9283f-s3-bucket-%s-%s-%d", cfg.Region, cfg.AccountId, i),
				s3Client:     s3.NewFromConfig(cfg.Config),
			})
		}
	}

	return results
}

type S3Bucket struct {
	utils.ThreadConfig
	bucketName string
	s3Client   *s3.Client
	thread     int
}

func (s *S3Bucket) Name() string {
	return fmt.Sprintf("s3-%s-%s-%d", s.AccountId, s.Region, s.thread)
}

// Setup creates the S3 bucket if it doesn't already exist.
func (s *S3Bucket) Setup(ctx *utils.Context) error {
	var conf *s3Types.CreateBucketConfiguration

	// us-east-1 doesn't need a LocationConstraint.
	if s.Region != "us-east-1" {
		conf = &s3Types.CreateBucketConfiguration{
			LocationConstraint: s3Types.BucketLocationConstraint(s.Region),
		}
	}

	_, err := s.s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket:                    &s.bucketName,
		CreateBucketConfiguration: conf,
	})
	if err != nil {
		// If we own the bucket, carry on.
		var ownedErr *s3Types.BucketAlreadyOwnedByYou
		if errors.As(err, &ownedErr) {
			ctx.Debug.Printf("Bucket already owned by us: %s", s.bucketName)
		} else {
			return fmt.Errorf("create bucket %s: %w", s.Name(), err)
		}
	}
	return nil
}

// ScanArn attempts to update the bucket policy using the given ARN.
// If the ARN is invalid (non-existent role), a "MalformedPolicy" error
// containing "invalid principal" is returned by AWS.
func (s *S3Bucket) ScanArn(ctx *utils.Context, arn string) (bool, error) {
	policyDoc, err := json.Marshal(utils.GenerateTrustPolicy(fmt.Sprintf("arn:aws:s3:::%s", s.bucketName), "*", arn))
	if err != nil {
		return false, fmt.Errorf("marshalling policy: %w", err)
	}

	_, err = s.s3Client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: &s.bucketName,
		Policy: aws.String(string(policyDoc)),
	})
	if err != nil {
		var oe *smithy.GenericAPIError
		if errors.As(err, &oe) && oe.ErrorCode() == "MalformedPolicy" {
			// Check for "invalid principal" in error message
			if strings.Contains(strings.ToLower(oe.ErrorMessage()), "invalid principal") {
				return false, nil
			}
		}
		return false, fmt.Errorf("updating bucket policy: %w", err)
	}

	return true, nil
}

// CleanUp deletes the bucket (and optionally the bucket policy first).
func (s *S3Bucket) CleanUp(ctx *utils.Context) error {
	// Remove the bucket policy
	s.s3Client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
		Bucket: &s.bucketName,
	})

	// Delete the bucket
	if _, err := s.s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: &s.bucketName,
	}); err != nil {
		return fmt.Errorf("deleting bucket: %w", err)
	}

	return nil
}
