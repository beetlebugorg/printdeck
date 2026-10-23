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
