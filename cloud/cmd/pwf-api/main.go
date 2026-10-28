// Command pwf-api is the Lambda function of the Printdeck cloud service. It serves the API
// (package api) behind a function URL. CloudFront sends it the requests of /api/*.
//
// The environment: BUCKET is the data bucket.
//
// The logs go to CloudWatch. They have fixed texts only: no request body, no key, no code.
package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"printdeck/cloud/api"
)

func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal("pwf-api: no AWS configuration")
	}
	s := &api.Server{Store: &api.S3Store{Client: s3.NewFromConfig(cfg), Bucket: os.Getenv("BUCKET")}}
	lambda.Start(func(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
		return serve(ctx, s, req), nil
	})
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
		out.Headers[k] = w.Header().Get(k)
	}
	return out
}
