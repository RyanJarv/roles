package scanner

import (
	"github.com/google/go-cmp/cmp"
	"github.com/ryanjarv/roles/pkg/utils"
	"testing"
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
