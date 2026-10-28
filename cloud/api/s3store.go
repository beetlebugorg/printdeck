package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// S3Store is a Store on an S3 bucket.
type S3Store struct {
	Client *s3.Client
	Bucket string
}

func (b *S3Store) Get(ctx context.Context, key string) ([]byte, string, error) {
	out, err := b.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: &b.Bucket, Key: &key})
	if err != nil {
		return nil, "", s3Error(err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", err
	}
	return data, aws.ToString(out.ETag), nil
}

func (b *S3Store) Put(ctx context.Context, key string, data []byte, opt PutOptions) (string, error) {
	in := &s3.PutObjectInput{Bucket: &b.Bucket, Key: &key, Body: bytes.NewReader(data)}
	if opt.ContentType != "" {
		in.ContentType = &opt.ContentType
	}
	if opt.CacheControl != "" {
		in.CacheControl = &opt.CacheControl
	}
	if opt.IfMatch != "" {
		in.IfMatch = &opt.IfMatch
	}
	if opt.IfNoneMatch {
		in.IfNoneMatch = aws.String("*")
	}
	out, err := b.Client.PutObject(ctx, in)
	if err != nil {
		return "", s3Error(err)
	}
	return aws.ToString(out.ETag), nil
}

func (b *S3Store) Delete(ctx context.Context, key string) error {
	_, err := b.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.Bucket, Key: &key})
	return s3Error(err)
}

func (b *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	p := s3.NewListObjectsV2Paginator(b.Client, &s3.ListObjectsV2Input{Bucket: &b.Bucket, Prefix: &prefix})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, s3Error(err)
		}
		for _, o := range page.Contents {
			out = append(out, aws.ToString(o.Key))
		}
	}
	return out, nil
}

// s3Error maps the errors of S3 to ErrNotFound and ErrConflict.
func s3Error(err error) error {
	if err == nil {
		return nil
	}
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) {
		switch re.HTTPStatusCode() {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusPreconditionFailed, http.StatusConflict:
			return ErrConflict
		}
	}
	var ae smithy.APIError
	if errors.As(err, &ae) && (ae.ErrorCode() == "NoSuchKey" || ae.ErrorCode() == "NotFound") {
		return ErrNotFound
	}
	return err
}
