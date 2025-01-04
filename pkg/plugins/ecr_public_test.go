package plugins

import "testing"

func TestECRPublicRepository_ScanArn(t *testing.T) {
	type fields struct {
		NewECRPublicInput NewECRPublicInput
		thread            int
		region            string
		repositoryName    string
		repositoryArn     string
		ecrPublicClient   *ecrpublic.Client
	}
	type args struct {
		ctx *utils.Context
		arn string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    bool
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ECRPublicRepository{
				NewECRPublicInput: tt.fields.NewECRPublicInput,
				thread:            tt.fields.thread,
				region:            tt.fields.region,
				repositoryName:    tt.fields.repositoryName,
				repositoryArn:     tt.fields.repositoryArn,
				ecrPublicClient:   tt.fields.ecrPublicClient,
			}
			got, err := r.ScanArn(tt.args.ctx, tt.args.arn)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScanArn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ScanArn() got = %v, want %v", got, tt.want)
			}
		})
	}
}
