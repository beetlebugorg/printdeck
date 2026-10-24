# Push relay

The relay gives the messages of a deck to the Printdeck app on an iPhone, through APNs. It is an
AWS Lambda function behind a function URL. The APNs key of the app is in a Secrets Manager secret,
so no deck has it. The messages are encrypted from the deck to the app, so the relay and Apple
cannot read them.

## How it works

1. The app gets its device token from iOS, and posts it to the relay (`POST /register`). The relay
   returns a send token: an HMAC of the device token with `relay_secret`. The relay keeps no list
   of devices.
2. The app pairs with the deck on the local network. The deck and the app agree on a message key
   of 32 bytes. The app gives the deck the device token, its environment and the send token.
3. At an event of a print, the deck encrypts the message with the message key, and posts it to
   the relay (`POST /push`). The relay checks the send token, and gives APNs a push with the
   encrypted message in the key `e`.
4. The notification service extension of the app decrypts `e`, and shows the message. The watch
   shows the notifications of the phone.

A new `relay_secret` makes all send tokens invalid. A deck that is lost is revoked with a new
pairing: the app makes a new message key.

## Requests

Both are `POST` with a JSON body, to the URL of the relay.

`/register`, from the app:

```json
{"device": "<device token in hex>", "env": "production"}
```

`env` is `sandbox` for a build that Xcode installs, and `production` for TestFlight and the App
Store. The reply is `{"send_token": "..."}`.

`/push`, from the deck:

```json
{"device": "...", "env": "production", "send_token": "...", "payload": "<base64>", "priority": 5}
```

`priority` is 1 to 5, as in ntfy; 1 to 3 gives APNs priority 5, and 4, 5 or none gives 10. The
reply is 200 for a sent push, 403 for a send token that is not valid, 410 when the app is gone
from the device (stop sending to it), and 400 or 502 for other errors, with `{"error": "..."}`.

## Pairing with a deck

The app pairs over the API of the deck, with the API key of the deck in the header `X-Api-Key`.
The app makes an X25519 key pair, and posts:

`POST /api/plugin/printdeck/push/pair`

```json
{"name": "Jeremy's iPhone", "device": "<device token in hex>", "env": "sandbox",
 "send_token": "<from /register>", "relay": "https://<the relay URL>", "public_key": "<base64>"}
```

`public_key` is the raw X25519 public key of the app, 32 bytes, in standard base64. The reply is
`{"id": "...", "public_key": "<base64>"}` with the X25519 public key of the deck. Both sides make
the message key with HKDF-SHA256: the input key is the X25519 shared secret, the salt is the bytes
of the device token (the hex decoded), the info is `printdeck push v1`, and the length is 32
bytes. In CryptoKit that is `sharedSecret.hkdfDerivedSymmetricKey(using: SHA256.self, salt:
deviceTokenBytes, sharedInfo: Data("printdeck push v1".utf8), outputByteCount: 32)`. Someone who
listens on the network during the pairing sees the two public keys only.

After a pairing, the deck sends the message "This phone gets the messages of your prints." A new
pairing of the same device token makes a new key, with the same ID. `GET
/api/plugin/printdeck/push` lists the phones (`id`, `name`, `env`, `added`), and `DELETE
/api/plugin/printdeck/push/<id>` removes one. Settings > Phones on the deck shows the same list.

## The message

The payload is AES-256-GCM with the message key: a 12-byte random nonce, then the ciphertext and
the 16-byte tag, in standard base64. The plaintext is JSON:

```json
{"title": "Print done", "body": "benchy.gcode · 1h 42m", "tags": "white_check_mark", "at": 1792790000}
```

`at` is the time of the event in Unix seconds. The payload is at most 3000 bytes. The push has the
alert "Printdeck: A message from your printer" and `mutable-content`, so iOS shows that text when
the extension cannot decrypt the message.

## Logs

The logs in CloudWatch have fixed texts only. No request body, token, payload or part of the
secret is in a log. The log group keeps 14 days.

## Deploy

You need Go, the AWS CLI, and an APNs key (a `.p8` file) from the developer account.

1. `relay/deploy.sh BUNDLE_ID` builds the function and makes the stack `printdeck-relay`, with an
   S3 bucket for the code. It prints the URL of the relay.
2. `relay/set-apple-key.sh KEY_ID TEAM_ID AuthKey_KEYID.p8` writes the key into the secret. Run it
   on the computer that has the `.p8` file. It does not print the key.

Run `deploy.sh` again after a change to the code. To remove the relay, delete the stack and the
bucket.

## Limits

The function URL has no AWS auth: a deck or an app cannot keep an AWS key or a shared key secret.
The send token is the auth of each push. Two limits keep a flood of requests from running up the
bill:

- `RELAY_CONCURRENCY` (default 5) is the most instances of the function at a time. AWS keeps 10
  instances of an account unreserved, so an account with a concurrency quota of 10 needs
  `RELAY_CONCURRENCY=0` for no limit, or a higher quota.
- `ALERT_EMAIL=you@example.com` makes a budget of `BUDGET_USD` (default 5) US dollars a month for
  the account. AWS mails the address at 80 % of it, and when the forecast passes it. AWS asks the
  address to confirm the first mail.

`POST /register` gives a send token for any device token, with no proof that the caller has the
device. Someone who knows a device token can then send pushes to it. They cannot read or write a
message: the extension cannot decrypt their payload, so iOS shows the placeholder alert.
