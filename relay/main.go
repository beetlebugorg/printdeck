package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// main reads the secret (SECRET_ARN) at the start, and serves the function URL. TOPIC is the
// bundle ID of the app.
//
// The logs go to CloudWatch. They have fixed texts only: no error text of a parser, no request
// body, no token, and no part of the secret.
func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal("relay: no AWS configuration")
	}
	out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: strPtr(os.Getenv("SECRET_ARN"))})
	if err != nil {
		log.Fatal("relay: the secret did not read")
	}
	var s Secret
	if err := json.Unmarshal([]byte(*out.SecretString), &s); err != nil {
		log.Fatal("relay: the secret is not JSON")
	}
	r := NewRelay(s, os.Getenv("TOPIC"))
	lambda.Start(func(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
		return handle(r, req), nil
	})
}

func strPtr(s string) *string { return &s }

// handle serves POST /register and POST /push.
func handle(r *Relay, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	if req.RequestContext.HTTP.Method != http.MethodPost {
		return reply(http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
	}
	body := []byte(req.Body)
	if req.IsBase64Encoded {
		b, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return reply(http.StatusBadRequest, map[string]string{"error": "bad body"})
		}
		body = b
	}
	if len(body) > 8192 {
		return reply(http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
	}
	switch req.RawPath {
	case "/register":
		var in RegisterRequest
		if json.Unmarshal(body, &in) != nil {
			return reply(http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		}
		t, err := r.Register(in)
		if err != nil {
			return errorReply(err)
		}
		return reply(http.StatusOK, map[string]string{"send_token": t})
	case "/push":
		var in PushRequest
		if json.Unmarshal(body, &in) != nil {
			return reply(http.StatusBadRequest, map[string]string{"error": "bad JSON"})
		}
		if err := r.Push(in); err != nil {
			return errorReply(err)
		}
		return reply(http.StatusOK, map[string]string{"status": "sent"})
	}
	return reply(http.StatusNotFound, map[string]string{"error": "not found"})
}

func errorReply(err error) events.LambdaFunctionURLResponse {
	var e *Error
	if errors.As(err, &e) {
		return reply(e.Status, map[string]string{"error": e.Msg})
	}
	log.Print("relay: internal error")
	return reply(http.StatusInternalServerError, map[string]string{"error": "internal error"})
}

func reply(status int, v any) events.LambdaFunctionURLResponse {
	b, _ := json.Marshal(v)
	return events.LambdaFunctionURLResponse{StatusCode: status, Headers: map[string]string{"content-type": "application/json"}, Body: string(b)}
}
