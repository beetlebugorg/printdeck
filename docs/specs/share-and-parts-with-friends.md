# Share links and Parts with Friends

Status: phases 1 to 3 are built, and from phase 5 the photos and the public address. Phases 4, 6
and 7 are open. "Changes from the plan" lists the choices of the build.

This plan adds two features to Printdeck. They use one small cloud service at
`printdeck.beetlebug.org`.

1. **Share link.** Tap Share on the print page. The deck makes a link such as
   `printdeck.beetlebug.org/s/k7m2q9x4tz8w3fhn5c6v`. The link shows the job page of the mock
   (https://claude.ai/artifact/RznSrMoU8SNStXLKYvQUzk): the camera, the progress, the steps and
   the time lapse, which grows with each layer. The link exists only after a tap on Share. The
   service deletes it 24 hours after the print ends.
2. **Parts with Friends.** A project is a set of parts that the owner and friends print
   together. Friends with an invite upload STL and 3MF files and photos. The Cura plugin shows
   each new model and opens it in Cura. A project can also have a public address, which shows
   the prints, the photos and the model pictures to anybody with the address.

## Rules

- **No cloud keys in Printdeck.** The deck, the image and the Cura plugin have no AWS key and
  no shared secret. The deck calls an HTTPS API. The API gives the deck short-lived upload
  grants.
- **Nothing goes up without a tap.** The deck uploads only after a tap: Share, Add to project,
  or New project from the build plate in Cura. Each tap covers one print or one set of models.
  The deck reads from the service (the inbox of new models) with no tap, and those reads send
  no print data.
- **No passwords.** People sign in with passkeys. The service stores no passwords and no email
  addresses.
- **Serverless.** No server runs all the time. With no prints and no visitors, the cost is the
  storage.
- **No new inbound connections to the deck.** The deck only makes outbound HTTPS requests.
  Nothing on the home network accepts connections from the internet.
- **Low CPU on the deck.** The deck uploads the JPEG frames of the camera as they are, and the
  time lapse frames as the time lapse runner made them. The deck does no encode for the cloud.
- **Minimal SD card writes.** The deck writes its identity once, at enrollment. The uploads
  come from memory.
- **Go for all new code.** The Lambda function, the deploy and admin tool, and the logic of the
  web pages are Go. The pages run Go compiled to WebAssembly, with `wasm_exec.js` from the Go
  release as the loader. Two parts stay in their languages: the Cura plugin is Python, because
  Cura loads only Python plugins, and the iPhone app is Swift. CloudFormation YAML is the only
  other text.
- **One owner for each service, many decks.** A service has one owner. The owner enrolls as
  many decks as they like. Other people deploy their own service for their decks, with their own
  AWS account and domain. The domain of the service is a deck setting, with
  `printdeck.beetlebug.org` as the default.

## The parts

```
 deck ──signed HTTPS──▶ API (Lambda function URL) ──▶ S3 (data)
  │                                                    ▲
  └──── S3 POST with a grant (status, frames, lapse) ──┘
                                                       │
 visitor ◀── CloudFront (printdeck.beetlebug.org) ◀── S3 (pages, share and public files)
 owner or friend, browser or iPhone app ──passkey session──▶ API ──▶ presigned URLs
 Cura plugin ──LAN──▶ deck ──signed HTTPS──▶ API (projects, new models)
```

| Part | Where | What it does |
|---|---|---|
| `cloud/cmd/pwf-api` | Go, AWS Lambda (arm64), function URL | Enrollment, passkeys and sessions, shares, projects, members, grants, the scheduled deletes, the checks and pictures of uploads |
| `cloud/cmd/pwf-web` | Go, compiled to WebAssembly | The logic of the share page, the project pages and the sign-in |
| `cloud/cmd/pwf` | Go, a command on the computer of the owner | Deploy the stack, make enrollment codes, list and remove decks and passkeys |
| `deck/internal/cloud` | Go | The types, the request signature and the client, shared by the deck, the API and the pages |
| `deck/internal/threemf` | Go | A 3MF reader and a mesh-only 3MF writer |
| `cloud/deploy/web` | Static HTML and CSS in S3 | The page shells, the "not found" page, `wasm_exec.js`, `apple-app-site-association` |
| `cloud/deploy/template.yaml` | CloudFormation | The bucket, CloudFront, the certificate, the function, the DNS record, the budget |
| Cura plugin | Python | The Projects panel, New project from the build plate, the "new model" message, the project line in the G-code |
| iPhone app | Swift | The Projects tab, sign-in with the passkey, Add photo from the Photos app |

The service is the Go module `printdeck/cloud` in `cloud/`, with a `replace` of `printdeck` to
`../deck`. Its import paths start with `printdeck/`, so it can import the internal packages of
the deck, and the AWS SDK stays out of the deck module. The API and the pages share packages with
the deck: the signature, the status types, the STL reader and renderer (`internal/stl`,
`internal/gfx`) and the 3MF reader. The deck binary does not link the API or the page code.
The passkey code in the API uses `github.com/go-webauthn/webauthn`, a new dependency.

The push relay (`relay/`) stays a separate stack. It has a different job, and it works now.

## Identity

### The deck

The deck has no cloud key. It has its own identity:

1. At the first use of the cloud, the deck makes an Ed25519 key pair (Go `crypto/ed25519`). The
   private key stays on the deck, in the data folder. It is written once.
2. The owner runs `pwf enroll-code`. The command writes a hash of a random single-use code to
   S3, and prints the code. The code expires after 1 hour.
3. On the deck, the owner opens Settings, then Cloud, then Connect, and types the code. The deck
   sends `POST /api/v1/decks` with the code, its public key and its name.
4. The API checks the code and deletes it. The API stores the public key in
   `decks/<deck id>.json`, and returns the deck ID.

Each later request of the deck has these headers:

- `X-Printdeck-Deck`: the deck ID.
- `X-Printdeck-Time`: the time of the request, in Unix seconds.
- `X-Printdeck-Request`: a random request ID, 96 bits.
- `X-Printdeck-Signature`: the Ed25519 signature of the method, the path, the time, the
  request ID and the SHA-256 of the body.

The API refuses a request with a time more than 5 minutes from its clock. It keeps the request
IDs of the last 10 minutes in the memory of the function, and refuses an ID that it saw. A new
instance of the function starts with no IDs, so this check is partial. TLS is the main guard
against a replay.

The deck gets its time from NTP. A deck with a wrong clock shows "Set the clock" in Settings,
Cloud.

To remove a deck, the owner runs `pwf decks remove <name>`. The deck can also send
`DELETE /api/v1/decks/self` (Settings, Cloud, Disconnect). Remove a deck if its SD card is
lost: the card has the private key.

**Why not AWS keys or IoT certificates:** an AWS key on the deck can be copied from the SD
card, and it gives direct access to AWS. An Ed25519 key gives access only to the API of this
service, and the owner can remove it in one step.

### People: passkeys and sessions

People have no accounts and no passwords. Each person signs in with a passkey (WebAuthn) for
the domain of the service. The passkey is in the password manager of the phone or the
computer, and Face ID or Touch ID unlocks it.

**The owner.**

1. On the deck: Settings, Cloud, Owner sign-in. The deck asks the API for a one-time
   registration code (a signed request) and shows a QR code with
   `https://printdeck.beetlebug.org/owner#code=<code>`. The code expires after 10 minutes.
2. The owner scans the code with the phone. The page asks the phone to make a passkey. The API
   stores the public key of the passkey in `owners/<credential id>.json`.
3. Later, the owner opens `printdeck.beetlebug.org` and taps Sign in. The API checks the
   passkey, and returns a session token.

The owner adds a passkey for each device in the same way. `pwf owners list` and
`pwf owners remove` manage them. A lost phone is one `pwf owners remove`.

**A friend.**

1. The owner makes an invite with the name of the friend. The invite link is
   `printdeck.beetlebug.org/j/<code>`. The code is single use and expires after 7 days.
2. The friend opens the link. The page shows "Jeremy invited you to Garden planters" and Join.
3. Join asks the phone to make a passkey. The API stores the public key of the passkey with the
   member, in `project.json`. The invite is then used. Somebody else with the same link sees
   "This invite is used".
4. Later, the friend opens the project and signs in with the passkey.

A passkey of a friend works for the projects of that friend only. To remove a friend, the owner
removes the member. The API deletes the public key of the passkey.

**Sessions.** A session token is an ID, an expire time and an HMAC of both, signed with a key
of the service. The key is in Secrets Manager, as `relay_secret` is for the relay. The deck
does not have it. A session lasts 12 hours. The page keeps the token in `sessionStorage`, so it
goes away when the tab closes.

**The iPhone app.** The app uses the same passkey through `ASAuthorizationController`. This
needs the associated domain `webcredentials:printdeck.beetlebug.org` in the app, and the file
`/.well-known/apple-app-site-association` on the service. An associated domain is fixed when
the app is built. So the app from the App Store signs in with passkeys only on
`printdeck.beetlebug.org`. For a service on another domain, the app uses a token from the deck,
which the deck gives at pairing on the local network. The owner removes that token with
`pwf owners remove`.

## Upload grants

The deck uploads to S3 directly. A Lambda call for each frame costs more than the upload
itself, so the deck uses an S3 POST policy instead:

- The API returns the URL of the bucket and the fields of a POST policy.
- The policy allows only keys that start with one prefix, for example `s/<share id>/`.
- The policy sets the largest file size (`content-length-range`), the content types, and an
  end time 1 hour later.
- The deck uses the grant for all uploads in that hour. The deck asks for a new grant 10
  minutes before the end time.

A 3-hour print uses about 4 API calls.

Browsers and the iPhone app upload one file at a time, with a presigned PUT URL from the API.
The URL is valid for 15 minutes.

## Feature 1: the share link

### The flow

1. **Share.** Share is a key on the print page and on the done page. The deck sends
   `POST /api/v1/shares` with the job name, the printer name and the camera views. The API
   makes the share ID: 100 random bits, in 20 lowercase base32 characters. The API writes:
   - `s/<id>`, a copy of the share page, with the content type `text/html`;
   - `s/<id>/share.json`, with the deck ID, the job name and the create time.

   The API returns the link and a grant. The deck shows the link as a QR code, with Copy and
   Stop sharing.
2. **During the print.** The deck uploads these files with the grant:
   - `status.json` every 5 s: the state, the progress, the layer, the time left, the
     temperatures, the steps, the "cool at" time, the frame number and the count of time lapse
     frames. The record is about 1 KB.
   - `cam-<view>.jpg` every 5 s for each view: the newest camera frame, as the camera sent it.
   - `lapse/<n>.jpg` after each time lapse frame: the frame that the time lapse runner adds to
     the video, with the same crop. So the time lapse on the page grows with each layer.
3. **The end of the print.** The deck uploads the last status and the last frames. The time
   lapse frames are already up. The deck sends `POST /api/v1/shares/<id>/end`. The API writes
   the end time and the expire time to `share.json`. The API makes an EventBridge Scheduler
   job that runs at the expire time.
4. **24 hours later.** The scheduled job calls the API, and the API deletes `s/<id>` and all
   files under `s/<id>/`. The link then shows the "not found" page.
5. **Stop sharing.** The deck sends `DELETE /api/v1/shares/<id>`. The API deletes the files at
   once.

Share on the done page, after the print, uploads the last status, the last frame and the time
lapse frames of the video. The 24 hours start at the tap.

An S3 lifecycle rule deletes objects under `s/` after 3 days. This rule is a second guard, for
a deck that stops in the middle of a print. Each upload during a print makes the status and
the frames new again. The time lapse frames of a print longer than 3 days can go before the
end of the print (open question 2).

### The paths

| Path | What it is |
|---|---|
| `/` | The home page: Sign in, and the projects of the person who signed in |
| `/s/<20 characters>` | A share |
| `/p/<20 characters>` | The public address of a project |
| `/x/<20 characters>` | The page of a project for the owner and the members, after sign-in |
| `/j/<code>` | An invite |
| `/owner` | The owner sign-in setup, with a code from the deck |
| `/api/v1/...` | The API |

Other prefixes are free for later features.

CloudFront gives `/s/<id>` the S3 object `s/<id>` with no function and no rewrite. An ID with
no object gets the "not found" page (a CloudFront custom error response for 403 and 404). So a
link is live only between Share and its delete.

### The share page

The share page is the mock, with real data:

- The page is an HTML shell and `pwf-web.wasm`. The Go code reads `status.json` every 5 s. It
  loads `cam-<view>.jpg?f=<frame number>` when the status has a new frame number.
- **The time lapse.** The page shows the frames `lapse/0.jpg` to `lapse/<n-1>.jpg` on a canvas,
  in a loop, at the frame rate of the deck video. When the count goes up, the page adds the new
  frames to the loop. The frames do not change, so CloudFront and the browser cache them for a
  long time.
- After the end, it shows the "Ready to pick up" banner.
- The page has no controls of the printer, no deck address and no other prints.
- It follows the deck colors and the day and night themes, as the mock does.
- **The size of the WebAssembly file.** A Go WebAssembly file is large. I have not measured
  ours. The page shows the status from the HTML shell first, and the browser caches the file.
  If the file is too large for a phone, TinyGo is the fallback.

## Feature 2: Parts with Friends

### What it is

A **project** is a set of parts that the owner and friends print together, for example
"Garden planters". The project page shows:

- **Models:** the STL and 3MF files of the project, each with its picture, its name, who added
  it and when, and the prints of it.
- **Prints:** each print that the owner added to the project, with the photo at the end, the
  time lapse, the date, the time, the material and the grams.
- **Photos:** pictures that the owner and the friends take with a phone, for example of the
  parts after the cleanup. A photo can go with a print or a model, or with the project only.
- **Printing now:** the live share link, while a print of the project runs and has a share.
- **Members:** for the owner and the members only.

### Who can do what

| Who | Auth | Can |
|---|---|---|
| The owner, on the deck | The deck signature | Make projects, add prints, pull models |
| The owner, in a browser or the iPhone app | Owner passkey | All: make, rename and delete projects, invite and remove friends, the public address, add models and photos, remove any model or photo |
| A friend | Member passkey | See the project, download models, add models and photos, remove their own models and photos |
| Anybody with the public address | None | See the public page: prints, photos, model pictures. No downloads of models. |

### The public address

A new project has no public address. The owner turns it on in the project settings.

- **On:** the API makes a public ID (100 random bits) and copies the public files of the
  project to `p/<public id>/`: `page.json`, the model pictures, the print frames and the
  photos. CloudFront serves them.
- **Off:** the API deletes `p/<public id>/`. Old links and old picture URLs stop working.
- **New address:** the API makes a new public ID, copies the files to it with S3 `CopyObject`
  on the server, and deletes the old prefix. Use it when the address went to the wrong person.

The public ID is separate from the project ID. So the public address gives no access to the
API, and a new address does not change the project for the members.

The public page shows the first names that the owner typed in the invites. It shows no member
list.

### Data in S3

The service keeps its data as JSON files in S3. S3 conditional writes (`If-Match` with the
ETag) keep two changes from overwriting each other. This plan has no database.

```
decks/<deck id>.json            public key, name, enroll time
owners/<credential id>.json     owner passkeys: public key, device name, time
codes/<sha256 of code>.json     enrollment and owner codes, until used or expired
s/<share id>                    the share page
s/<share id>/...                status, frames, time lapse frames

p/<public id>                   the public page of a project
p/<public id>/page.json         what visitors see: names, times, the keys of the pictures
p/<public id>/...               copies of the model pictures, print frames and photos

x/<project id>/project.json     name, members with their passkeys, invites (hashes), public ID
x/<project id>/page.json        what members see
x/<project id>/models/<model id>.json  name, note, size, added by, time, pulled
x/<project id>/models/<model id>.stl or .3mf   the clean file (see "Clean files")
x/<project id>/models/<model id>.png   the picture, drawn by the API
x/<project id>/prints/<print id>.json  from the history record of the deck
x/<project id>/prints/<print id>/photo.jpg and lapse/<n>.jpg
x/<project id>/photos/<photo id>/full.jpg and small.jpg
x/<project id>/uploads/<upload id>     a file as uploaded, until the API reads it
```

The S3 bucket blocks all public access. CloudFront reads the bucket with Origin Access Control.
The bucket policy allows CloudFront `s3:GetObject` on `s/*`, `p/*` and the static pages only.
CloudFront cannot list the bucket or read `x/`. Members and the owner get presigned URLs for
the files under `x/`, valid for 1 hour.

### Clean files

A model from a friend goes into Cura on the computer of the owner. Cura has its own readers.
The API sends Cura only files that it wrote itself:

- **STL:** the API reads the upload and writes a binary STL with the same triangles.
- **3MF:** the API reads the meshes, the build items and their transforms, and writes a new 3MF
  with those only. Cura settings, scripts, pictures and other parts of the upload are not in
  the new file. Open question 4 asks about the settings.
- **Photos:** the API decodes the photo, turns it upright from its EXIF orientation, and writes
  new JPEG files with no EXIF data. A photo from a phone often has the GPS position of the
  home. The new files do not.

The API deletes the upload after it writes the clean file. A file that the readers cannot read
is deleted, and the page shows "This file is not a valid STL or 3MF".

### Limits

| Limit | Default |
|---|---|
| Model upload | 100 MB |
| 3MF content after decompression | 500 MB, and 10 million triangles |
| Photo upload | 25 MB, and 50 megapixels from the header before the decode |
| Uploads for each member | 50 a day |
| Storage of a project | 5 GB |
| Models in a project | 200 |

### Headers of the pages

- `Content-Security-Policy`: scripts and WebAssembly from the domain of the service only, no
  inline scripts, `connect-src` to the domain and the S3 bucket only.
- `Referrer-Policy: no-referrer`, so a link on a page does not send the address of the page.
- `X-Robots-Tag: noindex` and `<meta name="robots" content="noindex">` on the share, project and
  invite pages.

### A friend adds a model

1. On the project page, the friend taps Add model and picks an `.stl` or `.3mf` file.
2. The page sends `POST /api/v1/projects/<id>/models` with the name, the size and an optional
   note. The API
   checks the session, the size and the limits. It returns a presigned PUT URL under
   `uploads/`.
3. The browser uploads the file to S3. The page sends `POST .../models/<model id>/done`.
4. The API reads the file, writes the clean file and the picture, and writes `page.json`.

The service does no slicing. The memory of the function sets the largest model that it can
read and draw. I have not measured this.

### Add a photo

From the page: a print, a model and the project each have Add photo. The page opens the camera
or the photo library of the phone (`<input type="file" accept="image/*">`).

From the iPhone app: in the Photos app, Share, then Printdeck. The share extension of the app
lists the projects and their prints and models. The app converts the photo to JPEG before the
upload.

Then the page or the app gets a presigned PUT URL from `POST /api/v1/projects/<id>/photos`,
uploads the file, and sends `POST .../photos/<photo id>/done`. The API writes the clean files.

The API reads JPEG and PNG only. For another type, such as an HEIC file from a browser, the page
shows "Share the photo as JPEG".

### The owner gets the model in Cura

The Cura plugin talks only to the deck, as it does now. The deck talks to the cloud.

1. While the deck is connected, it asks the API for new models every 5 minutes
   (`GET /api/v1/projects/inbox`). The reply lists the models that are not pulled yet. This is
   a read: it sends no print data.
2. The deck keeps the list in memory only.
3. A new model can send a push to the iPhone through the relay: "Sam added Planter tray to
   Garden planters".
4. The plugin asks the deck for the list (`GET /api/plugin/printdeck/projects/inbox`) when Cura
   starts and every minute after that. For a new model, Cura shows a message with Open.
5. Open: the plugin asks the deck for the file
   (`GET /api/plugin/printdeck/projects/models/<id>`). The deck gets a presigned URL from the
   API and replies with a redirect. Cura downloads the clean file from S3 and loads it on the
   build plate. The deck then marks the model as pulled.
6. The plugin puts the project ID and the model ID on the scene node. When Cura writes the
   G-code, the plugin adds this line at the top:
   `;Printdeck project: <project id> model: <model id>`.

### A print goes to the project

A print goes to a project only after a tap:

1. **Share during a print of a project.** The deck reads the project line of the G-code at the
   start of the print. The share sheet then has "Add to Garden planters", on by default. With
   it on, the project page shows the share under "Printing now", and the print goes to the
   project at the end.
2. **Add to project on the done page or the history page.** The project of the G-code is the
   first choice. A print with no project line can go to any project.

For a print in a project, the deck asks for a grant for `x/<id>/prints/<print id>/`. It uploads
the last frame of the Picture camera and the time lapse frames. The API writes the record
(`print.json`: file, result, time, material, grams, roll) and the page files. A print that was
shared has its frames under `s/` already. The API copies them on the server (S3 `CopyObject`),
so the deck does not upload them again.

## Screens

The mock of these screens: see "The mock" at the end of this section.

### Make a project on the phone (the main path)

The owner signs in once with Owner sign-in on the deck (see "People"). Then:

1. Open `printdeck.beetlebug.org`. Tap Sign in. Face ID unlocks the passkey.
2. **Your projects:** a card for each project, with its last photo, the count of models and
   prints, and the time of the last change. A project with a live share has a lit "Printing"
   mark. New project is at the top.
3. **New project:** type the name, for example "Garden planters", and an optional line of
   text. Tap Create.
4. The API makes `x/<id>/project.json`. The public address is off.
5. **The project page, empty:** the name, and three keys:
   - Invite a friend
   - Add a model
   - Public address (off)
6. **Invite a friend:** type the first name, for example "Sam". Tap Make invite. The page shows
   the link with Share (the share sheet of the phone: Messages, Mail) and Copy. The invite shows
   as "Sam, invited" in Members until Sam joins.
7. **Public address:** a switch. On shows the address with Copy, QR code and New address.

The iPhone app has the same screens in a Projects tab.

### Make a project in Cura

1. In the Printdeck stage, the Projects panel has New project from the build plate.
2. Type the name in Cura. Tap Create.
3. The plugin sends the name and the models of the build plate to the deck. The deck makes the
   project with a signed request, and uploads the models with a grant. The tap on Create is the
   consent for this upload.
4. Cura shows "Garden planters is ready" with Invite (opens the project page in the browser)
   and Copy address.

### Make a project on the deck

1. Settings, Projects, New project. Type the name on the touch keyboard.
2. The deck shows a QR code of the project page. Scan it, and the project opens on the phone,
   where you invite friends.

The Projects page of the deck lists the projects with a QR code for each. The project choice
also shows in the share sheet and on the done page under Add to project.

### A friend joins

1. Sam opens the invite link: "Jeremy invited you to Garden planters", with Join.
2. Join: the phone asks to save a passkey for `printdeck.beetlebug.org`. Face ID.
3. The project page opens, with Add model and Add photo.

### The public page

The name, the photos first, then the prints with their time lapse, then the model pictures.
It has no keys except Sign in (for members) at the bottom.

### The mock

A clickable mock of the phone screens: https://claude.ai/artifact/7HdLVznzBCptn6uRGzo3dS

The mock adds one field: a friend can write a note with a model, for example "Two of these, in
green". The note shows on the project page and in the Cura message.

## Cost

These are estimates from AWS list prices. I did not check the prices in this session. Check
them before the deploy.

| Item | Use | Estimate |
|---|---|---|
| S3 storage | 100 prints at 10 MB, 200 models at 5 MB, 300 photos at 1 MB: about 2.3 GB, plus copies for public addresses | about $0.05 to $0.10 a month |
| S3 uploads, one 3-hour shared print | 2 frames and a status every 5 s, and 300 time lapse frames: about 6,800 PUT and POST requests | about $0.035 |
| CloudFront, one viewer for 3 hours | about 4,500 requests and 300 MB | in the free tier |
| Lambda | a few thousand calls a month, and the inbox: 8,640 calls a month for each deck | in the free tier |
| EventBridge Scheduler | one job for each share | in the free tier |
| Route 53 | `beetlebug.org` is already a Route 53 zone (checked: the NS records are `awsdns`) | no new cost |
| ACM certificate | for `printdeck.beetlebug.org`, in us-east-1 for CloudFront | free |
| Secrets Manager | one secret, the session key | about $0.40 a month |

To limit the cost of misuse:

- The function has a reserved concurrency of 5, as the relay has. A flood of requests can then
  make the API unavailable for a time. It cannot make the bill large.
- A budget sends mail at 80 % of `BUDGET_USD` (default $5), as the relay does.
- The limits of the uploads (see "Limits").
- CloudFront caches `status.json` and the frames for 2 s, so many viewers make few S3 reads.
- A person with a share link or a public address can download the frames many times. The cost
  of CloudFront traffic past the free tier is then the risk. The budget mail is the alarm, and
  New address or Stop sharing is the fix. A rate limit with AWS WAF has a fixed cost each month,
  so this plan does not use it.

## The work on the deck

- `internal/cloud`: the identity, the signed client, the grants, the share uploader, the
  project inbox. The uploader runs only while a share is live. It uses the frames of the camera
  hub (`Peek`) and the frames of the time lapse runner, and needs no new decode.
- UI:
  - The Share key on the print page and on the done page.
  - The share sheet: the QR code, the link, Add to the project, Stop sharing.
  - The "Shared" mark.
  - Add to project on the done page and on the history page.
  - Settings, Cloud: Connect, the deck name, Owner sign-in, Disconnect.
  - Settings, Projects: the projects with their QR codes, New project.
  - The manual pages for each of these.
- The OctoPrint plugin API: the inbox, the model routes, and New project for Cura.
- iPhone pairing: the deck gives the app a token for a service on another domain.
- The settings file: the service domain, the deck ID and the private key, written at
  enrollment.

## Phases

1. **The service and the identity.** The stack, `pwf deploy`, the enrollment, the signed
   requests. Done when a deck enrolls, and a request with a bad signature gets 401.
2. **Share link.** The share page, status and frames, the growing time lapse, Stop sharing, the
   delete 24 hours after the end. Done when a phone outside the home network follows a print on
   the TAZ.
3. **Passkeys and projects.** Owner sign-in, the home page, New project, invites and member
   passkeys, the project page, Add to project from the deck.
4. **Models from friends.** STL and 3MF upload, the clean files, the 3MF reader and writer, the
   pictures, the inbox on the deck, the message and the Projects panel in Cura, the project
   line in the G-code, New project from the build plate.
5. **Photos and the public address.** Add photo, the clean JPEG files, the public address with
   on, off and New address.
6. **The iPhone app.** The Projects tab, the passkey sign-in, the share extension for photos.
7. **Push for a new model or photo**, through the relay.

Each phase is a set of commits on main, with tests. The API gets a test with a local S3
stand-in, as `relay_test.go` tests the relay. `pwf deploy` builds the function and the
WebAssembly file, uploads the pages, and deploys the stack with the AWS SDK for Go.

## Changes from the plan

The build of phase 3 made these choices:

- **The paths of the pages.** One app page has three addresses: `/app` (sign in, the projects,
  and a project at `/app#x=<id>`), `/owner#code=<code>`, and `/j#<code>` for an invite. The codes
  are after `#`, so the browser does not send them, and CloudFront needs no function. The home
  page `/` is the app page too.
- **The session key** is a random file in the private bucket (`keys/session`), made at the first
  use. It is not in Secrets Manager, which costs about $0.40 a month.
- **The state of a passkey ceremony** goes to the page as a token with an HMAC, so the API keeps
  no state between the begin and the finish.
- **Members** have one passkey for each project (open question 5).
- **A print of the deck** goes to a project with Share on the page of the project in the history.
  The deck makes the project at the first share, and sends the prints that are not up yet: the
  record, the picture of the model, and the frames of the time lapse as the video has them.
- **The photos** of a project are in the build: the owner and the members add them on the page
  of the project, and a member removes only their own photos.
- **The page of a print.** Each print of a project has a page with the look of the share page
  of a finished print: the time lapse with Pause and a slider, the readout, the model with the
  chart of the layers from the slicer, the details, the filament, and the photos of the print.
  Its address is `/app#x=<id>&p=<print>` for the members, and `/p/<public id>#p=<print>` on the
  public address. The deck sends the facts of the page with the print (the layers, the plan of
  the slicer as `plan.json`, the filament, the estimate, and the warm-up time). A deck with the
  older format sends its prints again at the next share.
- **The page of a project** shows the numbers of the project (the prints, the print time and the
  filament), a tile for each print, the photos, and the keys of the owner at the bottom.
- **The share link of a print in a project.** The project of a print is the pick on the deck (the
  status panel of the print page), or the line `;Printdeck project:` that the Cura plugin writes
  into the G-code. The share has no "Add to" switch. A share of a print in a project:
  - makes the project at the service when it is not there;
  - shows "A print of the project" with a link on the share page;
  - shows as a "Printing now" tile on the project page, until the end of the print;
  - follows a change of the project during the print.
- **A print goes to its project at its end**, with no tap. The deck sends it after its time lapse
  video, or 10 minutes after the end with no video. It sends prints of the last 6 hours only, and
  each print once. Share on the project in the history still sends the older prints.
- **The photos of a share** go to the print in the project: the photos before the print went up,
  and the photos after it.
- **The end of a share in a project.** At the end of the 24 hours, the service deletes the files
  of the share. The page of the share stays and sends the visitor to the print page: the public
  page when the project has a public address, else the page for the members. The delete of the
  project deletes the page of the share too. This answers open question 3.
- **The live print of a project.** While a print in a project prints, the deck sends its
  progress to the project each minute, and at once at a pause and at the end: the name, the
  start, the percent, the layer, the time left and the printer. At the first send, it uploads the
  picture of the model. The send has no camera pictures. The project page shows the print as a
  "Printing now" tile with no share link. After the end, the tile shows the result until the
  print comes to the project. A tile with no send for 5 minutes goes away.
- **The project page refreshes** each minute, and each 30 seconds while a print of the project
  prints. A tab in the background does not refresh.

## Open questions

1. **Model downloads for visitors.** The plan gives model files to members and the owner only.
   Visitors see the pictures. Do you want a "Download" for visitors on some projects?
2. **Very long prints.** The lifecycle rule deletes share files after 3 days, as a guard. Each
   time lapse frame is uploaded once, so the first frames of a print longer than 3 days are
   deleted during the print. The fix is a longer rule (7 days), with the 24-hour delete from the
   scheduled job as the normal path. Is 7 days acceptable for files of a deck that stopped?
3. **The share page of a project print.** When a shared print is in a project, the project page
   links to the share. After the 24 hours, the project keeps its copy of the frames. Is that
   what you want?
4. **Cura settings in 3MF files.** The clean 3MF has the meshes only, so the settings of a
   friend do not reach Cura. A project setting can keep the settings, with the risk of the Cura
   3MF reader. Do you want that setting?
5. **Members on more than one project.** A friend has one passkey for each project that they
   join. One passkey for all the projects of a friend needs a person record across projects.
   Is one passkey for each project acceptable at first?
