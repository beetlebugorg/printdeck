# Push relay

The relay sends the messages of a deck to the Printdeck app on an iPhone, through APNs. It is an
AWS Lambda function behind a function URL. The APNs key is in a Secrets Manager secret, so no
deck has it. The messages are encrypted from the deck to the app. The relay and Apple see only
the encrypted message.

## Where it runs

The relay is part of the cloud service (`cloud/`): the function of the service serves it at
`https://<domain>/api/v1/relay/`, for example `https://printdeck.beetlebug.org/api/v1/relay/`.
The app posts to `<relay>/register`, and the deck posts to `<relay>/push`.

The APNs key and the key of the send tokens are in the Secrets Manager secret `printdeck-relay`.
The first stack of the relay (`printdeck-relay`) made it. `pwf deploy` of the service finds that
stack and gives its secret and its bundle ID to the function, so the send tokens of the apps stay
valid.

To set the APNs key, run on the computer with the `.p8` file:

```sh
cd cloud
go run ./cmd/pwf relay set-apple-key KEY_ID TEAM_ID AuthKey_KEYID.p8
```

CloudFront shows the "not found" page of the service for 403 and 404, so the relay replies 401
for a bad send token or code, and 410 for no such path.

The first stack `printdeck-relay` still serves its function URL, for an app that has that URL.
Before you delete that stack, set `DeletionPolicy: Retain` on its secret
(`cloud/deploy/relay.yaml` has it) and update the stack, so that the secret stays.

## How it works

1. The app posts its device token to the relay (`POST <relay>/register`). The relay sends a code to the
   device in a background push. The app posts the token again with the code, and gets a send
   token (an HMAC of the device token with `relay_secret`).
2. The app pairs with the deck on the local network. The deck gets a message key, the device
   token and the send token.
3. At an event of a print, the deck encrypts the message and posts it to the relay
   (`POST <relay>/push`). The relay checks the send token and sends the push to APNs.
4. The notification service extension of the app decrypts the message.

A new `relay_secret` makes all send tokens invalid. The relay keeps no list of devices. The
CloudWatch logs have fixed texts only, for 14 days. The function URL has no AWS auth: the send
token is the auth of each push. A caller with only a device token can make the relay send that
device a background push with no alert.

## Requests

`POST <relay>/register`, from the app:

```json
{"device": "<device token in hex>", "env": "production"}
```

`env` is `sandbox` for a build from Xcode, and `production` for TestFlight and the App Store. The
reply is 202. The relay sends a background push with `"c": "<code>"`. The app posts the same body
with `"code": "<code>"`. The reply is `{"send_token": "..."}`, or 403 for a wrong code. A code is
valid for 10 to 20 minutes.

`POST <relay>/push`, from the deck:

```json
{"device": "...", "env": "production", "send_token": "...", "payload": "<base64>", "priority": 5}
```

`priority` is 1 to 5, as in ntfy. 1 to 3 gives APNs priority 5, and 4, 5 or none gives 10. The
reply is 200 for a sent push, 403 for a bad send token, and 410 when the app is gone from the
device. Other errors are 400 or 502, with `{"error": "..."}`.

## Pairing with a deck

`POST /api/plugin/printdeck/push/pair` on the deck, with the API key in `X-Api-Key`:

```json
{"name": "Jeremy's iPhone", "device": "<device token in hex>", "env": "sandbox",
 "send_token": "<from /register>", "relay": "https://<the relay URL>", "public_key": "<base64>"}
```

`public_key` is the raw X25519 public key of the app (32 bytes, standard base64). The reply is
`{"id": "...", "public_key": "<base64>"}` with the key of the deck. Both sides make the message
key with HKDF-SHA256:

- The input key is the X25519 shared secret.
- The salt is the bytes of the device token.
- The info is `printdeck push v1`.
- The length is 32 bytes.

`GET /api/plugin/printdeck/push` lists the phones. `DELETE /api/plugin/printdeck/push/<id>`
removes one.

## The message

The payload is AES-256-GCM with the message key: a 12-byte random nonce, then the ciphertext and
the 16-byte tag, in standard base64. The payload is at most 3000 bytes. The plaintext is JSON:

```json
{"title": "Print done", "body": "benchy.gcode · 1h 42m", "tags": "white_check_mark", "at": 1792790000}
```

`at` is the time of the event in Unix seconds. If the extension fails to decrypt the message,
iOS shows "Printdeck: A message from your printer".
