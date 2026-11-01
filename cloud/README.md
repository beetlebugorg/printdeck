# Printdeck cloud service

The cloud service of a Printdeck: share links of prints, and Parts with Friends. The plan is in
`docs/specs/share-and-parts-with-friends.md`. This version has the service and the identity of
the decks (phase 1).

The service is serverless. CloudFront serves the pages and the public files from one S3 bucket,
and sends `/api/*` to a Lambda function. The deck has no AWS key: it signs each request with an
Ed25519 key of its own (`deck/internal/cloud`).

This module imports packages of the deck module (`printdeck/internal/...`), with a `replace` to
`../deck`. The AWS SDK stays out of the deck module.

| Path | What it is |
|---|---|
| `api/` | The API: enrollment, signed requests, the S3 store |
| `cmd/pwf-api/` | The Lambda function |
| `cmd/pwf/` | The tool of the owner: deploy, enrollment codes, the decks |
| `deploy/template.yaml` | The CloudFormation template |
| `deploy/web/` | The static pages |

## Deploy

You need Go, AWS credentials for the account, and a Route 53 hosted zone for the domain. The
stack is in us-east-1, the region of the certificates of CloudFront.

```sh
cd cloud
go run ./cmd/pwf deploy -domain printdeck.example.org -alert-email you@example.org
```

The first deploy takes 5 to 30 minutes, for CloudFront and the certificate. Run it again after a
change to the code.

The deploy also needs `tar` with xz. It puts ffmpeg in the package of the function, for the
videos of the time lapses: the static build 7.0.2 for Linux arm64 from johnvansickle.com, under
the GPL, version 3. The function runs it as a separate program. The download stays in the cache
folder of your user (`printdeck/` in it), and the deploy checks its SHA-256.

## Time lapse videos

When a print with a time lapse is done, the function makes `lapse.mp4` (H.264) and `lapse.jpg`
(the last frame) from the frames, and deletes the frames. The prints from before this change keep
their frames until you run the backfill once, after the deploy:

```sh
go run ./cmd/pwf lapse-backfill -n   # count the prints with frames and no video
go run ./cmd/pwf lapse-backfill      # the function makes the videos, 4 at a time
```

`-parallel N` sets the count at the same time. `-local` makes the videos on this computer with
its ffmpeg. A print that fails keeps its frames, and a second run tries it again.

## Logs and backups

The logs of the function (`/aws/lambda/<stack>-api`) and of the first relay stack
(`/aws/lambda/printdeck-relay`) are kept 3 days. The stacks own both log groups. `pwf deploy`
applies the retention of the service from `deploy/template.yaml`. The relay stack has its own template: update it after a
change to `deploy/relay.yaml`.

```sh
aws cloudformation update-stack --region us-east-1 --stack-name printdeck-relay \
  --template-body file://deploy/relay.yaml --capabilities CAPABILITY_IAM \
  --parameters ParameterKey=BundleId,UsePreviousValue=true ParameterKey=CodeBucket,UsePreviousValue=true \
    ParameterKey=CodeKey,UsePreviousValue=true ParameterKey=Concurrency,UsePreviousValue=true \
    ParameterKey=AlertEmail,UsePreviousValue=true ParameterKey=BudgetUSD,UsePreviousValue=true
```

AWS Backup makes a backup of the data bucket each day at 07:00 UTC, and keeps each backup 7
days, in the vault `<stack>-data`. The bucket has versioning, which AWS Backup needs. A lifecycle
rule deletes an old version 1 day after a change or a delete.

To restore:

1. Find the recovery point:

   ```sh
   aws backup list-recovery-points-by-backup-vault --region us-east-1 --backup-vault-name printdeck-cloud-data
   ```

2. Get the restore settings of the point. The reply has the names of the settings that the
   restore uses:

   ```sh
   aws backup get-recovery-point-restore-metadata --region us-east-1 \
     --backup-vault-name printdeck-cloud-data --recovery-point-arn ARN
   ```

3. Restore into a new bucket, and keep the live bucket as it is. Set `NewBucket` to `true` and
   `DestinationBucketName` to a new name. To restore some files only, set `ItemsToRestore` to
   their keys or prefixes, for example `["x/<project>/"]`.

   ```sh
   aws backup start-restore-job --region us-east-1 --recovery-point-arn ARN \
     --iam-role-arn ROLE_ARN --metadata file://restore.json
   ```

   `ROLE_ARN` is the role `BackupRole` of the stack.

4. Copy the files that you need from the new bucket into the data bucket with `aws s3 cp` or
   `aws s3 sync`, then delete the new bucket.

A restore brings back the data of the people who deleted it after the backup. Delete their data
again after a restore (`PRIVACY.md`).

## Connect a deck

```sh
go run ./cmd/pwf enroll-code
```

On the deck, open Settings, then Cloud. Set the service to your domain, tap Connect, and type the
code. The code works once, for 1 hour.

`pwf decks` lists the decks. `pwf decks remove ID` removes one, for example after the loss of its
SD card: the card has the key of the deck.

## License

The service is part of Printdeck, under the GNU Affero General Public License, version 3. Each
page of the service links to its source.
