package scanner

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"iter"
	"sync"
	"time"
)

type NewScannerInput struct {
	Storage   *Storage
	Plugins   [][]plugins.Plugin
	Force     bool
	RateLimit int
}

func NewScanner(input *NewScannerInput) *Scanner {
	return &Scanner{
		rateLimit: input.RateLimit,
		storage:   input.Storage,
		force:     input.Force,
		Plugins:   utils.FlattenList(input.Plugins),
	}
}

type Scanner struct {
	storage   *Storage
	force     bool
	input     chan string
	results   chan Result
	Plugins   []plugins.Plugin
	rateLimit int
}

// ScanArns scans the given ARN for access points
func (s *Scanner) ScanArns(ctx *utils.Context, principalArns []string) iter.Seq2[string, bool] {
	return func(yield func(string, bool) bool) {
		rootArnMap := RootArnMap(ctx, principalArns)

		var rootArnsToScan []string
		var allAccountArns []string

		rateLimitBucket, cancel := rateLimiter(ctx, s.rateLimit)
		defer cancel()

		if s.force {
			rootArnsToScan = lo.Keys(rootArnMap)
		} else {
			for rootArn, accountArns := range rootArnMap {
				if status, err := s.storage.GetStatus(rootArn); err != nil {
					ctx.Error.Fatalf("GetStatus: %s", err)
				} else if status == PrincipalDoesNotExist {
					if !yield(rootArn, false) {
						return
					}
				} else if status == PrincipalExists {
					allAccountArns = append(allAccountArns, accountArns...)
					if !yield(rootArn, true) {
						return
					}
				} else if status == PrincipalUnknown {
					rootArnsToScan = append(rootArnsToScan, rootArn)
				} else {
					ctx.Error.Fatalf("unknown status: %d", status)
				}
			}
		}

		if len(rootArnsToScan) > 0 {
			ctx.Info.Printf("Scanning %d root ARNs", len(rootArnsToScan))

			for root := range scanWithPlugins(ctx, s.Plugins, rootArnsToScan, rateLimitBucket) {
				if root.Exists {
					allAccountArns = append(allAccountArns, rootArnMap[root.Arn]...)
				}
				s.storage.Set(root.Arn, root.Exists)

				if !yield(root.Arn, root.Exists) {
					return
				}
			}
		}

		var accountArnsToScan []string
		if s.force {
			accountArnsToScan = allAccountArns
		} else {
			for _, principalArn := range allAccountArns {
				if status, err := s.storage.GetStatus(principalArn); err != nil {
					ctx.Error.Fatalf("GetStatus: %s", err)
				} else if status == PrincipalUnknown {
					accountArnsToScan = append(accountArnsToScan, principalArn)
				} else {
					if !yield(principalArn, status == PrincipalExists) {
						return
					}
				}
			}
		}

		if len(accountArnsToScan) > 0 {
			ctx.Info.Printf("Scanning %d account ARNs", len(accountArnsToScan))

			for result := range scanWithPlugins(ctx, s.Plugins, accountArnsToScan, rateLimitBucket) {
				s.storage.Set(result.Arn, result.Exists)

				if !yield(result.Arn, result.Exists) {
					return
				}
			}
		}
	}
}

func rateLimiter(ctx *utils.Context, rateLimit int) (chan int, context.CancelFunc) {
	rateLimitContext, cancelFunc := ctx.WithCancel()

	rateLimitBucket := make(chan int, rateLimit)
	go func() {
		for rateLimitContext.IsRunning() {
			refillRateLimitBucket(rateLimitBucket, rateLimit)
			time.Sleep(1 * time.Second)
		}
	}()
	return rateLimitBucket, cancelFunc
}

func refillRateLimitBucket(rateLimitBucket chan int, tokens int) {
	go func() {
		for i := 0; i < tokens; i++ {
			select {
			case rateLimitBucket <- i:
			default:
				continue
			}
		}
	}()
}

func (s *Scanner) CleanUp(ctx *utils.Context) error {
	return nil
}

func scanWithPlugins(ctx *utils.Context, plugins []plugins.Plugin, principalArns []string, rateLimitBucket chan int) chan Result {
	queueSize := 10 * len(plugins)

	ctx.Debug.Printf("queue size: %d", queueSize)

	input := make(chan string, queueSize)
	results := make(chan Result, queueSize)

	processed := 0

	// Close the results channel when all plugins are done processing input.
	wg := sync.WaitGroup{}
	for _, plugin := range plugins {
		wg.Add(1)

		go func() {
			for principalArn := range input {
				<-rateLimitBucket
				if exists, err := plugin.ScanArn(ctx, principalArn); err != nil {
					ctx.Error.Printf("%s: scanning: %s", plugin.Name(), err)
				} else {
					if exists {
						ctx.Debug.Printf("found: %s", principalArn)
					} else {
						ctx.Debug.Printf("not found: %s", principalArn)
					}
					processed++

					results <- Result{Arn: principalArn, Exists: exists}
				}
			}
			ctx.Debug.Printf("%s: finished processing input", plugin.Name())

			wg.Done()
			ctx.Debug.Printf("%s: done", plugin.Name())
		}()
	}

	go func() {
		statsCancel := LogStats(ctx, &processed)

		for _, principalArn := range principalArns {
			input <- principalArn
		}

		close(input)

		wg.Wait() // Wait for all plugins to finish processing input.

		statsCancel()
		close(results)
	}()
	return results
}

// LogStats logs stats every 5 seconds until the context is done.
func LogStats(ctx *utils.Context, processed *int) context.CancelFunc {
	start := time.Now()

	ctx, cancelFunc := ctx.WithCancel()

	go func() {
		for {
			elapsed := time.Now().Sub(start)
			perSecond := float64(*processed) / elapsed.Seconds()
			select {
			case <-ctx.Done():
				break
			case <-time.After(5 * time.Second):
				ctx.Info.Printf("processed %d in %.1f seconds: %.1f/second", *processed, elapsed.Seconds(), perSecond)
			}
		}
	}()

	return cancelFunc
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
			result[rootArn] = append(result[rootArn], principalArn)
		}
	}

	return result
}
