//go:build minio_it

// 依赖本地 MinIO(127.0.0.1:9000,access/secret 均为 123,需有 bucket public)。
// 默认 go test 不会跑,需要显式带 tag:
//
//	go test -tags minio_it ./save/method/ -run TestUploadToS3 -v
package method

import (
	"testing"

	"github.com/beck-8/subs-check/config"
)

func TestUploadToS3(t *testing.T) {
	config.GlobalConfig.S3Endpoint = "127.0.0.1:9000"
	config.GlobalConfig.S3AccessID = "123"
	config.GlobalConfig.S3SecretKey = "123"
	config.GlobalConfig.S3Bucket = "public"
	config.GlobalConfig.S3UseSSL = false
	type args struct {
		data     []byte
		filename string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "TEST MINIO",
			args: args{
				data:     []byte("test"),
				filename: "test.yaml",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UploadToS3(tt.args.data, tt.args.filename); (err != nil) != tt.wantErr {
				t.Errorf("UploadToS3() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
