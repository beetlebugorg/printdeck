package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"time"

	"printdeck/internal/cloud"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

// Copy copies the file on the server, with no download.
func (b *S3Store) Copy(ctx context.Context, src, dst string, opt PutOptions) error {
	ct := opt.ContentType
	if ct == "" {
		ct = mime.TypeByExtension(path.Ext(dst))
	}
	in := &s3.CopyObjectInput{Bucket: &b.Bucket, Key: &dst, CopySource: aws.String(b.Bucket + "/" + url.PathEscape(src)),
		MetadataDirective: types.MetadataDirectiveReplace, ContentType: &ct}
	if opt.CacheControl != "" {
		in.CacheControl = &opt.CacheControl
	}
	_, err := b.Client.CopyObject(ctx, in)
	return s3Error(err)
}

// FileURL returns an address of the file at key that works for ttl, with no other auth: for the
// private files of a project.
func (g *S3Granter) FileURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	out, err := s3.NewPresignClient(g.Client).PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &g.Bucket, Key: &key},
		s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return out.URL, nil
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

// S3Granter makes upload grants of an S3 bucket: presigned POST policies, signed with the
// credentials of the function.
type S3Granter struct {
	Client *s3.Client
	Bucket string
}

// Grant returns a grant for the keys under prefix.
func (g *S3Granter) Grant(ctx context.Context, prefix string, maxBytes int64, ttl time.Duration) (cloud.Grant, error) {
	pc := s3.NewPresignClient(g.Client)
	key := prefix + "${filename}"
	out, err := pc.PresignPostObject(ctx, &s3.PutObjectInput{Bucket: &g.Bucket, Key: &key}, func(o *s3.PresignPostOptions) {
		o.Expires = ttl
		o.Conditions = []any{
			[]any{"starts-with", "$key", prefix},
			[]any{"content-length-range", 0, maxBytes},
			[]any{"starts-with", "$Content-Type", ""},
			[]any{"starts-with", "$Cache-Control", ""},
		}
	})
	if err != nil {
		return cloud.Grant{}, err
	}
	fields := map[string]string{}
	for k, v := range out.Values {
		if k != "key" {
			fields[k] = v
		}
	}
	return cloud.Grant{URL: out.URL, Fields: fields, Prefix: prefix, MaxBytes: maxBytes, Expires: time.Now().Add(ttl).UTC()}, nil
}
