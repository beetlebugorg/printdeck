package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// A deck has no cookies of the service: the address of a plate file is a presigned address of
// the bucket, also behind CloudFront.
func TestDirectURL(t *testing.T) {
	g := &S3Granter{Client: s3.New(s3.Options{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("AKID", "SECRET", "")}),
		Bucket: "bucket", Paths: true}
	ctx := context.Background()
	key := "x/p1/builds/b1/plate-1.gcode"
	if u, _ := g.FileURL(ctx, key, time.Hour); u != "/"+key {
		t.Errorf("the page address %q", u)
	}
	u, err := g.DirectURL(ctx, key, time.Hour)
	if err != nil || !strings.HasPrefix(u, "https://") || !strings.Contains(u, "plate-1.gcode") || !strings.Contains(u, "X-Amz-Signature=") {
		t.Errorf("the deck address %q %v", u, err)
	}
}
