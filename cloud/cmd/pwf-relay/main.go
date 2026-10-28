// Command pwf-relay is the Lambda function of the push relay (package relay). pwf relay deploy
// makes its stack.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"printdeck/cloud/relay"
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
	out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(os.Getenv("SECRET_ARN"))})
	if err != nil {
		log.Fatal("relay: the secret did not read")
	}
	var s relay.Secret
	if err := json.Unmarshal([]byte(*out.SecretString), &s); err != nil {
		log.Fatal("relay: the secret is not JSON")
	}
	r := relay.NewRelay(s, os.Getenv("TOPIC"))
	lambda.Start(func(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
		return relay.Handle(r, req), nil
	})
}
