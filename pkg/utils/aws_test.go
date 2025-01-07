package utils

import "testing"

func TestGenerateSubAccountEmail(t *testing.T) {
	type args struct {
		email string
		i     string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Test 1",
			args: args{
				i:     "1",
				email: "me+test@ryanjarv.sh",
			},
			want: "me+test-1@ryanjarv.sh",
		},
		{
			name: "Test 2",
			args: args{
				i:     "2",
				email: "me+test@ryanjarv.sh",
			},
			want: "me+test-2@ryanjarv.sh",
		},
		{
			name: "Test 3",
			args: args{
				i:     "3",
				email: "me@ryanjarv.sh",
			},
			want: "me+3@ryanjarv.sh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateSubAccountEmail(tt.args.email, tt.args.i); got != tt.want {
				t.Errorf("GenerateSubAccountEmail() got = %v, want %v", got, tt.want)
			}
		})
	}
}
