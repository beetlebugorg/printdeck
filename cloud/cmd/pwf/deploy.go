package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"printdeck/cloud/deploy"
)

type deployOptions struct {
	Stack, Domain, AlertEmail, Src string
	Budget, Concurrency            int
}

// runDeploy builds the function, uploads its zip, deploys the stack, and uploads the pages.
func runDeploy(ctx context.Context, cfg aws.Config, opt deployOptions) error {
	src, err := findSrc(opt.Src)
	if err != nil {
		return err
	}
	id, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("AWS credentials: %w (run aws login)", err)
	}
	account := aws.ToString(id.Account)
	zone, err := hostedZone(ctx, cfg, opt.Domain)
	if err != nil {
		return err
	}
	fmt.Println("Building the function")
	zipData, err := buildFunction(ctx, src)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(zipData)
	codeBucket := "printdeck-cloud-code-" + account + "-" + region
	codeKey := "pwf-api-" + hex.EncodeToString(sum[:8]) + ".zip"
	s3c := s3.NewFromConfig(cfg)
	if err := ensureBucket(ctx, s3c, codeBucket); err != nil {
		return err
	}
	if _, err := s3c.PutObject(ctx, &s3.PutObjectInput{Bucket: &codeBucket, Key: &codeKey, Body: bytes.NewReader(zipData)}); err != nil {
		return fmt.Errorf("upload the function: %w", err)
	}
	params := map[string]string{
		"Domain": opt.Domain, "HostedZoneId": zone, "CodeBucket": codeBucket, "CodeKey": codeKey,
		"Concurrency": strconv.Itoa(opt.Concurrency), "AlertEmail": opt.AlertEmail, "BudgetUSD": strconv.Itoa(opt.Budget),
	}
	fmt.Println("Deploying the stack " + opt.Stack + ". The first deploy takes 5 to 30 minutes: CloudFront and the certificate.")
	if err := deployStack(ctx, cfg, opt.Stack, params); err != nil {
		return err
	}
	out, err := stackOutputs(ctx, cfg, opt.Stack)
	if err != nil {
		return err
	}
	fmt.Println("Uploading the pages")
	if err := uploadWeb(ctx, s3c, out["Bucket"]); err != nil {
		return err
	}
	fmt.Println("Done:", out["Url"])
	return nil
}

// findSrc returns the folder of the cloud module: dir, or the current folder, or cloud/ in it.
func findSrc(dir string) (string, error) {
	cands := []string{dir}
	if dir == "" {
		cands = []string{".", "cloud"}
	}
	for _, d := range cands {
		b, err := os.ReadFile(filepath.Join(d, "go.mod"))
		if err == nil && bytes.HasPrefix(b, []byte("module printdeck/cloud\n")) {
			return filepath.Abs(d)
		}
	}
	return "", errors.New("run pwf in the cloud folder of the Printdeck repository, or give -src")
}

// buildFunction builds cmd/pwf-api for Lambda (linux/arm64) and returns the zip of the function.
func buildFunction(ctx context.Context, src string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "pwf-build")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	bin := filepath.Join(tmp, "bootstrap")
	cmd := exec.CommandContext(ctx, "go", "build", "-tags", "lambda.norpc", "-trimpath", "-ldflags", "-s -w", "-o", bin, "./cmd/pwf-api")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go build: %w", err)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "bootstrap", Method: zip.Deflate}
	h.SetMode(0o755)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// hostedZone returns the ID of the Route 53 zone of the domain: the zone with the longest name
// that ends the domain.
func hostedZone(ctx context.Context, cfg aws.Config, domain string) (string, error) {
	r := route53.NewFromConfig(cfg)
	best, bestName := "", ""
	p := route53.NewListHostedZonesPaginator(r, &route53.ListHostedZonesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("Route 53 zones: %w", err)
		}
		for _, z := range page.HostedZones {
			if z.Config != nil && z.Config.PrivateZone {
				continue
			}
			name := strings.TrimSuffix(aws.ToString(z.Name), ".")
			if (domain == name || strings.HasSuffix(domain, "."+name)) && len(name) > len(bestName) {
				best, bestName = strings.TrimPrefix(aws.ToString(z.Id), "/hostedzone/"), name
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no Route 53 zone for %s in this account", domain)
	}
	return best, nil
}

// ensureBucket makes the private bucket of the code when it does not exist.
func ensureBucket(ctx context.Context, c *s3.Client, bucket string) error {
	if _, err := c.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket}); err == nil {
		return nil
	}
	if _, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
		return fmt.Errorf("make the bucket %s: %w", bucket, err)
	}
	t := true
	_, err := c.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{Bucket: &bucket,
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls: &t, IgnorePublicAcls: &t, BlockPublicPolicy: &t, RestrictPublicBuckets: &t}})
	return err
}

// deployStack makes or changes the stack, and waits for the end.
func deployStack(ctx context.Context, cfg aws.Config, stack string, params map[string]string) error {
	cf := cloudformation.NewFromConfig(cfg)
	var ps []cftypes.Parameter
	for k, v := range params {
		ps = append(ps, cftypes.Parameter{ParameterKey: aws.String(k), ParameterValue: aws.String(v)})
	}
	caps := []cftypes.Capability{cftypes.CapabilityCapabilityIam}
	_, err := cf.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{StackName: &stack})
	const wait = 60 * time.Minute
	if err != nil {
		if _, err := cf.CreateStack(ctx, &cloudformation.CreateStackInput{StackName: &stack,
			TemplateBody: aws.String(deploy.Template), Parameters: ps, Capabilities: caps}); err != nil {
			return fmt.Errorf("make the stack: %w", err)
		}
		if err := cloudformation.NewStackCreateCompleteWaiter(cf).Wait(ctx,
			&cloudformation.DescribeStacksInput{StackName: &stack}, wait); err != nil {
			return fmt.Errorf("the stack did not complete: %w (see the events of the stack in the AWS console)", err)
		}
		return nil
	}
	if _, err := cf.UpdateStack(ctx, &cloudformation.UpdateStackInput{StackName: &stack,
		TemplateBody: aws.String(deploy.Template), Parameters: ps, Capabilities: caps}); err != nil {
		if strings.Contains(err.Error(), "No updates are to be performed") {
			return nil
		}
		return fmt.Errorf("change the stack: %w", err)
	}
	if err := cloudformation.NewStackUpdateCompleteWaiter(cf).Wait(ctx,
		&cloudformation.DescribeStacksInput{StackName: &stack}, wait); err != nil {
		return fmt.Errorf("the change of the stack did not complete: %w", err)
	}
	return nil
}

// uploadWeb uploads the static pages to the root of the data bucket.
func uploadWeb(ctx context.Context, c *s3.Client, bucket string) error {
	return fs.WalkDir(deploy.Web, "web", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := deploy.Web.ReadFile(p)
		if err != nil {
			return err
		}
		key := strings.TrimPrefix(p, "web/")
		ctype := mime.TypeByExtension(path.Ext(key))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		cache := "public, max-age=300"
		if strings.HasPrefix(key, "assets/") {
			cache = "public, max-age=3600"
		}
		_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: &bucket, Key: &key, Body: bytes.NewReader(data),
			ContentType: &ctype, CacheControl: &cache})
		return err
	})
}
