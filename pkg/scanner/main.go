package scanner

import (
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/samber/lo"
	"iter"
	"sync"
	"time"
)

const ConcurrencyMultiplier = 1

type NewScannerInput struct {
	Concurrency int
	Storage     *Storage
	Plugins     [][]plugins.Plugin
	Force       bool
}

func NewScanner(input *NewScannerInput) *Scanner {
	return &Scanner{
		storage: input.Storage,
		force:   input.Force,
		Plugins: utils.FlattenList(input.Plugins),
	}
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
			ctx.Info.Printf("Scanning %d root ARNs", len(rootArnsToScan))

			for root := range scanWithPlugins(ctx, s.Plugins, rootArnsToScan) {
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

			for result := range scanWithPlugins(ctx, s.Plugins, accountArnsToScan) {
				s.storage.Set(result.Arn, result.Exists)

				if !yield(result.Arn, result.Exists) {
					return
				}
			}
		}
	}
}

func (s *Scanner) CleanUp(ctx *utils.Context) error {
	for _, plugin := range s.Plugins {
		if p, ok := plugin.(plugins.CleanUpPlugin); ok {
			ctx.Info.Printf("cleaning up %s", plugin.Name())
			if err := p.CleanUp(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func scanWithPlugins(ctx *utils.Context, plugins []plugins.Plugin, principalArns []string) chan Result {
	queueSize := 0
	for _, plugin := range plugins {
		queueSize += plugin.Concurrency() * 2 * ConcurrencyMultiplier
	}

	ctx.Debug.Printf("queue size: %d", queueSize)

	input := make(chan string, queueSize)
	results := make(chan Result, queueSize)

	MustSetupPlugins(ctx, plugins)

	processed := 0

	// Close the results channel when all plugins are done processing input.
	wg := sync.WaitGroup{}
	for _, plugin := range plugins {
		for i := 0; i < plugin.Concurrency(); i++ {
			wg.Add(1)

			go func(i int) {
				for principalArn := range input {
					if exists, err := plugin.ScanArn(ctx, principalArn); err != nil {
						ctx.Error.Printf("%s-%d: scanning: %s", plugin.Name(), i, err)
					} else {
						if exists {
							ctx.Debug.Printf("thread %d found: %s", i, principalArn)
						} else {
							ctx.Debug.Printf("thread %d not found: %s", i, principalArn)
						}
						processed++

						results <- Result{Arn: principalArn, Exists: exists}
					}
				}
				ctx.Debug.Printf("%s-%d: finished processing input", plugin.Name(), i)

				wg.Done()
				ctx.Debug.Printf("%s-%d: done", plugin.Name(), i)
			}(i)
		}
	}

	start := time.Now()

	go func() {
		for {
			time.Sleep(5 * time.Second)
			LogStats(ctx, start, time.Now(), processed)
		}
	}()

	go func() {
		for _, principalArn := range principalArns {
			input <- principalArn
		}

		close(input)

		wg.Wait() // Wait for all plugins to finish processing input.
		LogStats(ctx, start, time.Now(), processed)
		close(results)
	}()
	return results
}

func LogStats(ctx *utils.Context, start time.Time, now time.Time, processed int) {
	elapsed := now.Sub(start)
	perSecond := float64(processed) / elapsed.Seconds()
	ctx.Info.Printf("processed %d in %.1f seconds: %.1f/second", processed, elapsed.Seconds(), perSecond)
}

func MustSetupPlugins(ctx *utils.Context, plugins []plugins.Plugin) {
	setupWg := sync.WaitGroup{}

	for _, plugin := range plugins {
		setupWg.Add(1)
		go func() {
			if err := plugin.Setup(ctx); err != nil {
				ctx.Error.Fatalf("%s: setting up plugin: %s", plugin.Name(), err)
			}
			setupWg.Done()
		}()
	}

	// Wait for all plugins to finish setting up.
	setupWg.Wait()
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
