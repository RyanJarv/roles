package scanner

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"github.com/ryanjarv/roles/pkg/utils"
	"testing"
	"time"
)

func TestRootArnMap(t *testing.T) {
	type args struct {
		ctx           *utils.Context
		principalArns []string
	}
	tests := []struct {
		name string
		args args
		want map[string][]string
	}{
		{
			name: "TestRootArnMap",
			args: args{
				ctx: &utils.Context{},
				principalArns: []string{
					"arn:aws:iam::123456789012:role/a",
				},
			},
			want: map[string][]string{
				"arn:aws:iam::123456789012:root": {
					"arn:aws:iam::123456789012:role/a",
				},
			},
		},
		{
			name: "TestRootArnMap",
			args: args{
				ctx: &utils.Context{},
				principalArns: []string{
					"arn:aws:iam::123456789012:role/a",
					"arn:aws:iam::123456789012:role/b",
				},
			},
			want: map[string][]string{
				"arn:aws:iam::123456789012:root": {
					"arn:aws:iam::123456789012:role/a",
					"arn:aws:iam::123456789012:role/b",
				},
			},
		},
		{
			name: "TestRootArnMap",
			args: args{
				ctx: &utils.Context{},
				principalArns: []string{
					"arn:aws:iam::123456789012:role/a",
					"arn:aws:iam::123456789012:role/b",
					"arn:aws:iam::123456789013:role/a",
					"arn:aws:iam::123456789013:role/b",
				},
			},
			want: map[string][]string{
				"arn:aws:iam::123456789012:root": {
					"arn:aws:iam::123456789012:role/a",
					"arn:aws:iam::123456789012:role/b",
				},
				"arn:aws:iam::123456789013:root": {
					"arn:aws:iam::123456789013:role/a",
					"arn:aws:iam::123456789013:role/b",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, RootArnMap(tt.args.ctx, tt.args.principalArns)); diff != "" {
				t.Errorf("RootArnMap() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRateLimiter_NoMock tests rateLimiter without mocking utils.Context.
func TestRateLimiter_NoMock(t *testing.T) {
	// 1. Create a real utils.Context. You might have a helper like:
	//    func NewContext(parent context.Context) *Context
	//    If so, pass in context.Background() or an existing parent context.
	parentCtx := utils.NewContext(context.Background()) // or utils.NewContext(context.Background()), etc.

	// 2. Derive a cancelable context from it—this should be the same call your rateLimiter does internally.
	rateLimitCtx, cancelFunc := parentCtx.WithCancel()

	// 3. Choose a rate limit.
	rateLimit := 5

	// 4. Invoke the rateLimiter function under test.
	bucket, bucketCancel := rateLimiter(rateLimitCtx, rateLimit)

	// Give the background goroutine a moment to fill the bucket.
	time.Sleep(1200 * time.Millisecond)

	// 5. Read all available tokens to see how many we got.
	tokens := 0
readLoop:
	for {
		select {
		case <-bucket:
			tokens++
		default:
			break readLoop
		}
	}

	if tokens != rateLimit {
		t.Fatalf("expected bucket to have %d tokens, got %d", rateLimit, tokens)
	}

	// 6. Check that it refills after another second.
	// Drain any leftover (should be zero).
	drained := 0
drainLoop:
	for {
		select {
		case <-bucket:
			drained++
		default:
			break drainLoop
		}
	}
	if drained != 0 {
		t.Errorf("expected 0 leftover tokens after drain, got %d", drained)
	}

	// Sleep again for refill.
	time.Sleep(1200 * time.Millisecond)

	tokens = 0
readLoop2:
	for {
		select {
		case <-bucket:
			tokens++
		default:
			break readLoop2
		}
	}
	if tokens < rateLimit {
		t.Fatalf("expected at least %d tokens on refill, got %d", rateLimit, tokens)
	}

	// 7. Cancel the context to stop refilling.
	cancelFunc()   // Cancel the context you derived above.
	bucketCancel() // Also call the cancelFunc returned by rateLimiter if needed.

	// Wait a bit for the goroutine to exit.
	time.Sleep(500 * time.Millisecond)

	// 8. Ensure no more tokens arrive after cancellation.
	time.Sleep(1200 * time.Millisecond)

	tokensAfterCancel := 0
readLoop3:
	for {
		select {
		case <-bucket:
			tokensAfterCancel++
		default:
			break readLoop3
		}
	}
	if tokensAfterCancel > 0 {
		t.Errorf("expected 0 tokens after cancel, got %d", tokensAfterCancel)
	}
}
