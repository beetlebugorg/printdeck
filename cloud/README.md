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
