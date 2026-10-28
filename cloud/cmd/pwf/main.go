// Command pwf is the tool of the owner of a Printdeck cloud service. It runs on the computer of
// the owner, with the AWS credentials of the owner.
//
//	pwf deploy [-domain D] [-alert-email A] [-budget N] [-concurrency N]
//	                         build the function and deploy the stack and the pages
//	pwf enroll-code          make a single-use code to connect a deck (Settings, Cloud, Connect)
//	pwf decks                list the connected decks
//	pwf decks remove ID      remove a deck, for example after the loss of its SD card
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
	fs.Parse(args)
	ctx := context.Background()
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
	case "decks":
		err = decks(ctx, cfg, *stack, fs.Args())
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pwf deploy | enroll-code | decks [remove ID]")
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
