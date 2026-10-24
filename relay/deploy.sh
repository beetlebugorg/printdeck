#!/bin/sh
# Builds the relay and deploys it with CloudFormation, in the region of the AWS CLI.
#
#   relay/deploy.sh BUNDLE_ID
#
# RELAY_CONCURRENCY (default 5, 0 for no limit) is the most instances of the function at a time.
# ALERT_EMAIL makes a budget of BUDGET_USD a month (default 5) that mails this address.
#
# The first run makes an S3 bucket for the code (printdeck-relay-code-ACCOUNT-REGION) and the stack
# printdeck-relay. A later run changes the stack to the new code. It needs Go and the AWS CLI.
set -eu
RELAY="$(cd "$(dirname "$0")" && pwd)"
BUNDLE_ID="$1"
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
REGION=$(aws configure get region)
BUCKET="printdeck-relay-code-$ACCOUNT-$REGION"

BUILD=$(mktemp -d)
trap 'rm -rf "$BUILD"' EXIT
(cd "$RELAY" && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -o "$BUILD/bootstrap" .)
(cd "$BUILD" && zip -q relay.zip bootstrap)
KEY="relay-$(sha256sum "$BUILD/relay.zip" | cut -c1-16).zip"

if ! aws s3api head-bucket --bucket "$BUCKET" 2>/dev/null; then
  if [ "$REGION" = us-east-1 ]; then
    aws s3api create-bucket --bucket "$BUCKET" >/dev/null
  else
    aws s3api create-bucket --bucket "$BUCKET" --create-bucket-configuration LocationConstraint="$REGION" >/dev/null
  fi
  aws s3api put-public-access-block --bucket "$BUCKET" --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
fi
aws s3 cp --quiet "$BUILD/relay.zip" "s3://$BUCKET/$KEY"

aws cloudformation deploy --stack-name printdeck-relay --template-file "$RELAY/template.yaml" \
  --capabilities CAPABILITY_IAM --no-fail-on-empty-changeset \
  --parameter-overrides BundleId="$BUNDLE_ID" CodeBucket="$BUCKET" CodeKey="$KEY" \
    Concurrency="${RELAY_CONCURRENCY:-5}" AlertEmail="${ALERT_EMAIL:-}" BudgetUSD="${BUDGET_USD:-5}"
aws cloudformation describe-stacks --stack-name printdeck-relay \
  --query 'Stacks[0].Outputs[?OutputKey==`RelayUrl`].OutputValue' --output text
