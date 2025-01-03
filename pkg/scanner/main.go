package scanner

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"iter"
	"sync"
)

const ConcurrencyMultiplier = 1

type NewScannerInput struct {
	Config      aws.Config
	Concurrency int
	Storage     *Storage
	Plugins     []plugins.Plugin
	Force       bool
}

func NewScanner(ctx *utils.Context, input *NewScannerInput) (*Scanner, error) {
	scanner := &Scanner{
		storage: input.Storage,
		force:   input.Force,
		Plugins: input.Plugins,
	}

	return scanner, nil
}

type Scanner struct {
	storage *Storage
	force   bool
	input   chan string
	results chan Result
	Plugins []plugins.Plugin
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
			for root := range scanWithPlugins(ctx, rootArnsToScan, s.Plugins) {
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
			for result := range scanWithPlugins(ctx, accountArnsToScan, s.Plugins) {
				s.storage.Set(result.Arn, result.Exists)

				if !yield(result.Arn, result.Exists) {
					return
				}
			}
		}
	}
}

func scanWithPlugins(ctx *utils.Context, principalArns []string, plugins []plugins.Plugin) chan Result {
	queueSize := 0
	for _, plugin := range plugins {
		queueSize += plugin.Concurrency() * 2 * ConcurrencyMultiplier
	}

	input := make(chan string, queueSize)
	results := make(chan Result, queueSize)

	// Close the results channel when all plugins are done processing input.
	wg := sync.WaitGroup{}

	for _, plugin := range plugins {
		if err := plugin.Setup(ctx); err != nil {
			ctx.Error.Fatalf("%s: setting up plugin: %s", plugin.Name(), err)
		}

		for i := 0; i < plugin.Concurrency(); i++ {
			wg.Add(1)

			go func(i int) {
				for arn := range input {
					if exists, err := plugin.ScanArn(ctx, arn); err != nil {
						ctx.Error.Fatalf("%s-%d: scanning: %s", plugin.Name(), i, err)
					} else {
						results <- Result{Arn: arn, Exists: exists}
					}
				}
				ctx.Debug.Printf("%s-%d: finished processing input", plugin.Name(), i)

				wg.Done()
				ctx.Debug.Printf("%s-%d: done", plugin.Name(), i)
			}(i)
		}
	}

	go func() {
		for _, principalArn := range principalArns {
			input <- principalArn
		}

		close(input)

		wg.Wait() // Wait for all plugins to finish processing input.
		close(results)
	}()
	return results
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
