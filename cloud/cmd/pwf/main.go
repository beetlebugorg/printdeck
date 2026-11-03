// Command pwf is the tool of the owner of a Printdeck cloud service. It runs on the computer of
// the owner, with the AWS credentials of the owner.
//
//	pwf deploy [-domain D] [-alert-email A] [-budget N] [-concurrency N]
//	                         build the function and deploy the stack and the pages
//	pwf enroll-code          make a single-use code to connect a deck (Settings, Cloud, Connect)
//	pwf decks                list the connected decks
//	pwf decks remove ID      remove a deck, for example after the loss of its SD card
//	pwf relay set-apple-key KEY_ID TEAM_ID AuthKey_KEYID.p8
//	                         write the APNs key of the app into the secret of the push relay
//	pwf dev [-addr A]        run the whole service on this computer, with the data in memory
//	pwf lapse-backfill [-n] [-local] [-parallel N]
//	                         make the time lapse videos of the old prints, and delete their frames
//
// All commands take -stack (default printdeck-cloud). The stack is in us-east-1, the region of
// the certificates of CloudFront.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"printdeck/cloud/api"
)

// region is the region of the stack: CloudFront takes its certificates from us-east-1 only.
const region = "us-east-1"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	stack := fs.String("stack", "printdeck-cloud", "the name of the CloudFormation stack")
	var opt deployOptions
	if cmd == "deploy" {
		fs.StringVar(&opt.Domain, "domain", "printdeck.beetlebug.org", "the domain of the service")
		fs.StringVar(&opt.AlertEmail, "alert-email", os.Getenv("ALERT_EMAIL"), "the address of the budget alarm, or none")
		fs.IntVar(&opt.Budget, "budget", 5, "the budget of the account in a month, in US dollars")
		fs.IntVar(&opt.Concurrency, "concurrency", 0, "the most instances of the function, or 0 for no limit")
		fs.StringVar(&opt.Src, "src", "", "the folder of the cloud module (default: found from the current folder)")
	}
	var appID, appKey *string
	if cmd == "github-app" {
		appID = fs.String("id", "", "the App ID of the GitHub App (its settings page shows it)")
		appKey = fs.String("key", "", "the private key of the App: the .pem file from GitHub")
	}
	ctx := context.Background()
	if cmd == "dev" {
		if err := runDev(ctx, args); err != nil {
			fatal(err)
		}
		return
	}
	var lapse lapseOptions
	if cmd == "lapse-backfill" {
		fs.BoolVar(&lapse.Local, "local", false, "make the videos on this computer with its ffmpeg")
		fs.IntVar(&lapse.Parallel, "parallel", 4, "the most videos at the same time")
		fs.BoolVar(&lapse.List, "n", false, "only count the prints")
	}
	fs.Parse(args)
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		fatal(err)
	}
	opt.Stack = *stack
	switch cmd {
	case "deploy":
		err = runDeploy(ctx, cfg, opt)
	case "enroll-code":
		err = enrollCode(ctx, cfg, *stack)
	case "relay":
		err = runRelay(ctx, cfg, fs.Args())
	case "decks":
		err = decks(ctx, cfg, *stack, fs.Args())
	case "github-app":
		err = runGitHubApp(ctx, cfg, *stack, *appID, *appKey)
	case "lapse-backfill":
		err = runLapseBackfill(ctx, cfg, *stack, lapse)
	case "rename-project":
		err = renameProject(ctx, cfg, *stack, fs.Args())
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pwf deploy | enroll-code | decks [remove ID] | relay set-apple-key | github-app --id N --key app.pem | dev | lapse-backfill | rename-project SLUG NAME")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "pwf:", err)
	os.Exit(1)
}

// stackOutputs returns the outputs of the stack.
func stackOutputs(ctx context.Context, cfg aws.Config, stack string) (map[string]string, error) {
	out, err := cloudformation.NewFromConfig(cfg).DescribeStacks(ctx, &cloudformation.DescribeStacksInput{StackName: &stack})
	if err != nil {
		return nil, fmt.Errorf("the stack %s: %w (run pwf deploy first)", stack, err)
	}
	m := map[string]string{}
	for _, o := range out.Stacks[0].Outputs {
		m[aws.ToString(o.OutputKey)] = aws.ToString(o.OutputValue)
	}
	return m, nil
}

// dataStore returns the data bucket of the stack.
func dataStore(ctx context.Context, cfg aws.Config, stack string) (*api.S3Store, error) {
	out, err := stackOutputs(ctx, cfg, stack)
	if err != nil {
		return nil, err
	}
	if out["Bucket"] == "" {
		return nil, fmt.Errorf("the stack %s has no bucket", stack)
	}
	return &api.S3Store{Client: s3.NewFromConfig(cfg), Bucket: out["Bucket"]}, nil
}

func enrollCode(ctx context.Context, cfg aws.Config, stack string) error {
	st, err := dataStore(ctx, cfg, stack)
	if err != nil {
		return err
	}
	code, err := api.NewCode(ctx, st, "deck", time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("%s\n\nType this code on the deck: Settings, Cloud, Connect.\nIt works once, until %s.\n",
		code, time.Now().Add(api.CodeTTL).Format("15:04"))
	return nil
}

func decks(ctx context.Context, cfg aws.Config, stack string, args []string) error {
	st, err := dataStore(ctx, cfg, stack)
	if err != nil {
		return err
	}
	if len(args) == 2 && args[0] == "remove" {
		list, err := api.Decks(ctx, st)
		if err != nil {
			return err
		}
		for _, d := range list {
			if d.ID == args[1] {
				if err := api.RemoveDeck(ctx, st, d.ID); err != nil {
					return err
				}
				fmt.Printf("Removed %s (%s).\n", d.Name, d.ID)
				return nil
			}
		}
		return fmt.Errorf("no deck %s", args[1])
	}
	if len(args) != 0 {
		usage()
	}
	list, err := api.Decks(ctx, st)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("No decks. Connect one with pwf enroll-code.")
	}
	for _, d := range list {
		fmt.Printf("%-18s %-24s connected %s\n", d.ID, d.Name, d.Enrolled.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

// renameProject gives the project of a slug a new name (api.Server.RenameProject).
func renameProject(ctx context.Context, cfg aws.Config, stack string, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: pwf rename-project SLUG NAME")
	}
	out, err := stackOutputs(ctx, cfg, stack)
	if err != nil {
		return err
	}
	st, err := dataStore(ctx, cfg, stack)
	if err != nil {
		return err
	}
	s := &api.Server{Store: st, Base: out["Url"]}
	slug, err := s.RenameProject(ctx, args[0], args[1])
	if err != nil {
		return err
	}
	fmt.Printf("%s is now %q at /projects/%s\n", args[0], args[1], slug)
	return nil
}
