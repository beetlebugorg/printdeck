// Command pwf-api is the Lambda function of the Printdeck cloud service. It serves the API
// (package api) behind a function URL. CloudFront sends it the requests of /api/*.
//
// The environment: BUCKET is the data bucket, and DOMAIN the domain of the service.
// RELAY_SECRET_ARN and TOPIC (the bundle ID of the app) turn on the push relay at /api/v1/relay/. A schedule
// sends the event {"pwf": "sweep"} every 15 minutes, which deletes the shares after their time.
// The comments on pull requests run after their request, in an event invoke of this function.
//
// The logs go to CloudWatch. They have fixed texts only: no request body, no key, no code.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"printdeck/cloud/api"
	"printdeck/cloud/relay"
)

func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal("pwf-api: no AWS configuration")
	}
	client := s3.NewFromConfig(cfg)
	bucket := os.Getenv("BUCKET")
	s := &api.Server{Store: &api.S3Store{Client: client, Bucket: bucket}, Grants: &api.S3Granter{Client: client, Bucket: bucket},
		Domain: os.Getenv("DOMAIN")}
	// The signed cookies of the files of the projects (filecookie.go), when the stack has the key.
	if arn, id := os.Getenv("FILES_KEY_SECRET"), os.Getenv("FILES_KEY_ID"); arn != "" && id != "" {
		out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &arn})
		if err != nil {
			log.Fatal("pwf-api: the key of the files did not read")
		}
		key, err := api.ParseFileKey(*out.SecretString)
		if err != nil {
			log.Fatalf("pwf-api: the key of the files: %v", err)
		}
		s.Files = &api.FileSigner{Domain: s.Domain, KeyID: id, Key: key}
		s.Grants.(*api.S3Granter).Paths = true
	}
	// The GitHub App that starts the slice runs of the builds, when the stack has it.
	if arn := os.Getenv("GITHUB_APP_SECRET"); arn != "" {
		out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &arn})
		if err != nil {
			log.Fatal("pwf-api: the secret of the GitHub App did not read")
		}
		var sec api.GitHubAppSecret
		if err := json.Unmarshal([]byte(*out.SecretString), &sec); err != nil {
			log.Fatal("pwf-api: the secret of the GitHub App is not JSON")
		}
		key, err := api.ParseFileKey(sec.PrivateKey)
		if err != nil {
			log.Fatalf("pwf-api: the key of the GitHub App: %v", err)
		}
		s.GitHub = &api.GitHubApp{AppID: sec.AppID, Key: key}
	}
	// The push relay of the iPhone app, with the secret of the relay: its APNs key and the key
	// of its send tokens.
	if arn := os.Getenv("RELAY_SECRET_ARN"); arn != "" {
		out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &arn})
		if err != nil {
			log.Fatal("pwf-api: the secret of the relay did not read")
		}
		var sec relay.Secret
		if err := json.Unmarshal([]byte(*out.SecretString), &sec); err != nil {
			log.Fatal("pwf-api: the secret of the relay is not JSON")
		}
		s.Relay = relay.NewRelay(sec, os.Getenv("TOPIC"))
	}
	// The jobs after a request run in another run of this function: an event invoke.
	if name := os.Getenv("AWS_LAMBDA_FUNCTION_NAME"); name != "" {
		lc := awslambda.NewFromConfig(cfg)
		s.Later = func(ctx context.Context, job api.Job) error {
			b, err := json.Marshal(jobEvent{Pwf: "job", Job: &job})
			if err != nil {
				return err
			}
			_, err = lc.Invoke(ctx, &awslambda.InvokeInput{FunctionName: &name, InvocationType: types.InvocationTypeEvent, Payload: b})
			return err
		}
	}
	lambda.Start(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var ev jobEvent
		if json.Unmarshal(raw, &ev) == nil && ev.Pwf == "job" && ev.Job != nil {
			s.RunJob(ctx, *ev.Job)
			return map[string]bool{"ok": true}, nil
		}
		if json.Unmarshal(raw, &ev) == nil && ev.Pwf == "sweep" {
			if err := s.Sweep(ctx); err != nil {
				log.Print("pwf-api: the sweep failed")
				return nil, err
			}
			return map[string]bool{"ok": true}, nil
		}
		var req events.LambdaFunctionURLRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		return serve(ctx, s, req), nil
	})
}

// jobEvent is the event of the schedule ({"pwf": "sweep"}) and of a job after a request
// ({"pwf": "job", "job": {...}}).
type jobEvent struct {
	Pwf string   `json:"pwf"`
	Job *api.Job `json:"job,omitempty"`
}

// serve runs a request of the function URL through the http.Handler of the API.
func serve(ctx context.Context, h http.Handler, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	body := req.Body
	if req.IsBase64Encoded {
		b, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return events.LambdaFunctionURLResponse{StatusCode: http.StatusBadRequest}
		}
		body = string(b)
	}
	target := req.RawPath
	if req.RawQueryString != "" {
		target += "?" + req.RawQueryString
	}
	r := httptest.NewRequestWithContext(ctx, req.RequestContext.HTTP.Method, target, strings.NewReader(body))
	for k, v := range req.Headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	out := events.LambdaFunctionURLResponse{StatusCode: w.Code, Headers: map[string]string{}, Body: w.Body.String()}
	for k := range w.Header() {
		// A function URL takes the cookies in their own list: in the headers, only one of the
		// three signed cookies of a project went to the browser.
		if http.CanonicalHeaderKey(k) == "Set-Cookie" {
			out.Cookies = w.Header().Values(k)
			continue
		}
		out.Headers[k] = w.Header().Get(k)
	}
	return out
}
