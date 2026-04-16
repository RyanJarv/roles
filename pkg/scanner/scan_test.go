package scanner

import (
	"context"
	"fmt"
	"github.com/ryanjarv/roles/pkg/plugins"
	"github.com/ryanjarv/roles/pkg/utils"
	"github.com/stretchr/testify/assert"
	"testing"
)

// mockPlugin implements plugins.Plugin for testing scanWithPlugins.
type mockPlugin struct {
	name string
	// scanFunc is called for each ARN. If nil, returns (true, nil).
	scanFunc func(arn string) (bool, error)
}

func (m *mockPlugin) Name() string { return m.name }
func (m *mockPlugin) Setup(_ *utils.Context) error {
	return nil
}
func (m *mockPlugin) ScanArn(_ *utils.Context, arn string) (bool, error) {
	if m.scanFunc != nil {
		return m.scanFunc(arn)
	}
	return true, nil
}
func (m *mockPlugin) CleanUp(_ *utils.Context) error { return nil }

// unlimitedBucket returns a rate-limit bucket that never blocks.
func unlimitedBucket() chan int {
	ch := make(chan int, 1000)
	for i := 0; i < 1000; i++ {
		ch <- i
	}
	return ch
}

// TestScanWithPlugins_RetriesErroredArn verifies that transient plugin errors
// cause the ARN to be retried rather than emitted as a false negative.
func TestScanWithPlugins_RetriesErroredArn(t *testing.T) {
	ctx := utils.NewContext(context.Background())

	arns := []string{
		"arn:aws:iam::111111111111:role/GoodRole",
		"arn:aws:iam::111111111111:role/RetryRole",
		"arn:aws:iam::111111111111:role/AnotherGoodRole",
	}

	attempts := map[string]int{}

	// Single plugin that errors once for one ARN, then succeeds on retry.
	plugin := &mockPlugin{
		name: "test-plugin",
		scanFunc: func(arn string) (bool, error) {
			attempts[arn]++
			if arn == "arn:aws:iam::111111111111:role/RetryRole" && attempts[arn] == 1 {
				return false, fmt.Errorf("simulated transient AWS error")
			}
			return true, nil
		},
	}

	results := scanWithPlugins(ctx, []plugins.Plugin{plugin}, arns, unlimitedBucket())

	got := map[string]bool{}
	for r := range results {
		got[r.Arn] = r.Exists
	}

	assert.Equal(t, 2, attempts["arn:aws:iam::111111111111:role/RetryRole"],
		"expected transient error to trigger a retry")

	// Every input ARN must appear in results once a retry succeeds.
	for _, arn := range arns {
		_, ok := got[arn]
		assert.True(t, ok, "ARN %q missing from results", arn)
	}

	// All ARNs should be reported as existing after the retry succeeds.
	assert.True(t, got["arn:aws:iam::111111111111:role/GoodRole"])
	assert.True(t, got["arn:aws:iam::111111111111:role/RetryRole"])
	assert.True(t, got["arn:aws:iam::111111111111:role/AnotherGoodRole"])
}

// TestScanWithPlugins_PersistentErrorsAreNotEmitted verifies that when every
// attempt errors, the ARN is left unresolved instead of being emitted as false.
func TestScanWithPlugins_PersistentErrorsAreNotEmitted(t *testing.T) {
	ctx := utils.NewContext(context.Background())

	arn := "arn:aws:iam::111111111111:role/ErrorRole"
	attempts := 0
	plugin := &mockPlugin{
		name: "test-plugin",
		scanFunc: func(gotArn string) (bool, error) {
			assert.Equal(t, arn, gotArn)
			attempts++
			return false, fmt.Errorf("persistent AWS error")
		},
	}

	results := scanWithPlugins(ctx, []plugins.Plugin{plugin}, []string{arn}, unlimitedBucket())

	got := []Result{}
	for r := range results {
		got = append(got, r)
	}

	assert.Equal(t, maxScanAttempts, attempts)
	assert.Empty(t, got, "persistent errors should not emit a false-negative result")
}

// TestScanWithPlugins_AllResultsReturned verifies every ARN produces a result
// under normal (no-error) conditions.
func TestScanWithPlugins_AllResultsReturned(t *testing.T) {
	ctx := utils.NewContext(context.Background())

	arns := []string{
		"arn:aws:iam::111111111111:role/RoleA",
		"arn:aws:iam::111111111111:role/RoleB",
		"arn:aws:iam::111111111111:role/RoleC",
	}

	// Two plugins that always succeed — exercises the multi-plugin path.
	p1 := &mockPlugin{name: "plugin-1", scanFunc: func(arn string) (bool, error) { return true, nil }}
	p2 := &mockPlugin{name: "plugin-2", scanFunc: func(arn string) (bool, error) { return false, nil }}

	results := scanWithPlugins(ctx, []plugins.Plugin{p1, p2}, arns, unlimitedBucket())

	got := map[string]bool{}
	for r := range results {
		got[r.Arn] = r.Exists
	}

	assert.Len(t, got, len(arns), "expected one result per input ARN")
	for _, arn := range arns {
		_, ok := got[arn]
		assert.True(t, ok, "ARN %q missing from results", arn)
	}
}
