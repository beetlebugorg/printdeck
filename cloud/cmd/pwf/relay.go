package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// relaySecret is the name of the secret of the push relay. The first stack of the relay
// (printdeck-relay) made it, and the service reads it.
const relaySecret = "printdeck-relay"

// relayFunctions are the functions that read the secret of the relay at their start.
var relayFunctions = []string{"printdeck-relay", "printdeck-cloud-api"}

// runRelay serves pwf relay set-apple-key KEY_ID TEAM_ID AuthKey_KEYID.p8: it writes the APNs key
// into the secret of the relay, and starts the functions again so that they read it. The key goes
// from the .p8 file to Secrets Manager in memory: pwf does not print it. relay_secret stays as
// it is, so the send tokens of the apps stay valid.
func runRelay(ctx context.Context, cfg aws.Config, args []string) error {
	if len(args) != 4 || args[0] != "set-apple-key" {
		return fmt.Errorf("usage: pwf relay set-apple-key KEY_ID TEAM_ID AuthKey_KEYID.p8")
	}
	keyID, teamID, file := args[1], args[2], args[3]
	p8, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sm := secretsmanager.NewFromConfig(cfg)
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(relaySecret)})
	if err != nil {
		return fmt.Errorf("the secret %s: %w", relaySecret, err)
	}
	var sec map[string]any
	if err := json.Unmarshal([]byte(aws.ToString(out.SecretString)), &sec); err != nil {
		return fmt.Errorf("the secret %s is not JSON", relaySecret)
	}
	sec["key_id"], sec["team_id"], sec["p8"] = keyID, teamID, string(p8)
	b, _ := json.Marshal(sec)
	if _, err := sm.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{SecretId: aws.String(relaySecret), SecretString: aws.String(string(b))}); err != nil {
		return err
	}
	// A change of the configuration starts new instances, which read the new secret.
	lc := lambda.NewFromConfig(cfg)
	desc := fmt.Sprintf("APNs key %s, set %s", keyID, time.Now().UTC().Format("2006-01-02T15:04Z"))
	for _, fn := range relayFunctions {
		if _, err := lc.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{FunctionName: aws.String(fn), Description: aws.String(desc)}); err != nil {
			fmt.Printf("The function %s did not start again: %v\n", fn, err)
		}
	}
	fmt.Printf("The relay has the APNs key %s of the team %s.\n", keyID, teamID)
	return nil
}
