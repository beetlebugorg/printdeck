package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"printdeck/cloud/api"
)

// githubAppName is the secret of the GitHub App of the stack: its ID and its private key.
func githubAppName(stack string) string { return stack + "-github-app" }

// runGitHubApp serves pwf github-app --id <App ID> --key <private key .pem>: it keeps the App in
// the secret <stack>-github-app. The next pwf deploy gives the secret to the function, which then
// starts the slice runs of the builds.
func runGitHubApp(ctx context.Context, cfg aws.Config, stack, id, keyFile string) error {
	appID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || appID <= 0 || keyFile == "" {
		return errors.New("usage: pwf github-app --id <App ID> --key <private key .pem>")
	}
	pemText, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	if _, err := api.ParseFileKey(string(pemText)); err != nil {
		return fmt.Errorf("the key: %w", err)
	}
	body, _ := json.Marshal(api.GitHubAppSecret{AppID: appID, PrivateKey: string(pemText)})
	text := string(body)
	sm := secretsmanager.NewFromConfig(cfg)
	name := githubAppName(stack)
	_, err = sm.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{SecretId: &name, SecretString: &text})
	var nf *smtypes.ResourceNotFoundException
	if errors.As(err, &nf) {
		_, err = sm.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: &name, SecretString: &text,
			Description: aws.String("The GitHub App of " + stack + ": it starts the slice runs of the builds")})
	}
	if err != nil {
		return fmt.Errorf("the secret %s: %w", name, err)
	}
	fmt.Printf("The GitHub App %d is in the secret %s. Run pwf deploy, so the function uses it.\n", appID, name)
	return nil
}

// githubAppSecret returns the ARN of the secret of the GitHub App of the stack, or "" when the
// stack has no App.
func githubAppSecret(ctx context.Context, cfg aws.Config, stack string) (string, error) {
	name := githubAppName(stack)
	out, err := secretsmanager.NewFromConfig(cfg).DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: &name})
	var nf *smtypes.ResourceNotFoundException
	if errors.As(err, &nf) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return aws.ToString(out.ARN), nil
}
