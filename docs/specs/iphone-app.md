# The iPhone app: several printers, the service, projects and the queue

Status: proposed, 2026-10-31.

The iPhone and Watch app in `watch/` shows one deck on the home network. This spec grows it into
the app for all the printers and for the projects of the service, at home and away. The work is in
four phases. Each phase works alone, and each phase ships.

The app side is Swift in `watch/`. The deck side and the service side are Go in `deck/` and
`cloud/`. Each phase lists the work of each side.

## Rules for each phase

- Only a press on the touch screen of a deck starts a print from the queue. The app shows the
  queue, and it can reorder and remove files. It has no Start for a queued file.
- The app never starts a resin print, and never resumes one. It has no Print key and no Resume key
  for a resin deck. On the Phenom, M6030 and a resume turn on the UV light, and a start with no
  build plate in place damages the printer. Pause and Cancel work.
- A deck key stays on the iPhone, in the keychain group of the app. The service never gets it.
- The session token of the service stays in the keychain. The app signs in again with the passkey
  when the token expires.

## Phase 1: several printers

The home screen has a card for each deck. A card shows the state, the print, the time left, the
progress and a camera picture. A tap opens the deck screen of today.

App:

- Keep a list of decks in place of one deck. Each deck has its address, its key, its name, its
  process (fdm or resin) and its camera choice. Move the settings of today into the first deck of
  the list.
- Bonjour (`_octoprint._tcp`) finds the decks. Connect adds a deck to the list. The deck screen has
  Remove.
- Read each deck every 5 seconds while the home screen shows, as today for one deck.
- The Live Activity shows the deck that prints. With two prints, it shows the print that ends
  first. The widgets get a setting for the deck, with "the deck that prints" as the default.
- The watch shows the deck that prints, or the first deck. The Digital Crown screens stay as they
  are.
- A resin deck has no Print a file, no Resume and no roll screen. `process` in
  `GET /api/plugin/printdeck` is `fdm` or `resin`.

Deck (Go), done in commit after 298a7852:

- The resin deck serves the app API (`newResinAPI` in `cmd/printdeck/web.go`), with the same API
  keys and the same App Keys flow:
  - `GET /api/plugin/printdeck`: the status of the print, the LCD panel, and `process`.
  - `GET /api/job`, and `POST /api/job` with pause and cancel. A resume, or a toggle of a paused
    print, gets 409.
  - The push pairing routes `/api/plugin/printdeck/push…`.
  - `GET /webcam/*` for a deck with a camera.
  - An upload, and a file select with print, get 409.
- The resin deck has no `model.png` yet.
- Bonjour finds both decks as `_octoprint._tcp`. The process comes from the status route.

## Phase 2: sign-in to the service, at home and away

The app signs in to printdeck.beetlebug.org with the passkey of the owner or of a member, the same
passkey as the web site. Away from the home network, a deck card shows the live tile of the
service: the print, the progress, the time left and the picture of the model. At home the app
reads the decks.

App:

- Add the entitlement `com.apple.developer.associated-domains` with
  `webcredentials:printdeck.beetlebug.org`.
- Sign in with `ASAuthorizationPlatformPublicKeyCredentialProvider` (relying party
  `printdeck.beetlebug.org`):
  1. `POST /api/v1/auth/login/begin` gives the options and a sealed state.
  2. The app gets an assertion from the passkey.
  3. `POST /api/v1/auth/login/finish` with the state and the assertion gives a `SignInReply`:
     the token, the kind (owner or member), the name, and the expiry time.
- Send `Authorization: Bearer <token>` on each request. A 401 means: sign in again.
- `GET /api/v1/projects` lists the projects of the session. `GET /api/v1/projects/<id>` gives the
  view of a project: the prints, the photos, the live prints, the plans, the printers and the queue.
- A deck that does not answer on the home network uses the live prints of the projects. For the
  owner, `GET /api/v1/printers` also has `live` for each printer: what it prints now, with a
  project or not (name, percent, layer, layers, left seconds, paused, the last result and its
  end, and the count and the time of the files of its queue), and `updated`. A member gets 401 on
  this route: a project sees its own prints only.
- `GET /api/plugin/printdeck` has `printer_id`: the ID of the printer at the service. It matches
  `printer_id` of the live prints and `id` of `GET /api/v1/printers`.

Service (Go):

- Serve `/.well-known/apple-app-site-association` with
  `{"webcredentials":{"apps":["7UZF9976U3.org.beetlebug.printdeck"]}}`, with the content type
  `application/json`. `pwf deploy` uploads it.
- An owner session works for 12 hours. Phase 2 keeps that. A later change can add a longer token
  for the app, kept in the keychain, if the sign-in each day is too often.

## Phase 3: projects

The app shows the overview of a project, as the web page does:

- What prints now, and the plates that wait for an approval.
- Ready for pickup, with Delivered.
- The files of the project in the queues, with their places and the estimates of their starts.
- The newest prints, and a feed of what happened.

The routes are those of the web page:

- A print: `PATCH /api/v1/projects/<id>/prints/<print>` with `ready`, `delivered`, `test`,
  `title`, `notes` or `source`. Only the owner sets the marks and the title.
- A plate: `POST /api/v1/projects/<id>/builds/<build>/plates/<n>/approve` or `reject` (with a
  reason), and `send` with a printer of the same process. Only the owner does these.
- Photos: `POST /api/v1/projects/<id>/photos` gives a grant. The app posts the JPEG to the grant,
  then `POST /api/v1/projects/<id>/photos/<photo>/done`, with the print of the photo or none. Take
  photos keeps the camera open for the next photo, and sends each photo after it is taken.
- The build page has a 3D preview of the G-code on the web. The app can open the web page for it
  in a web view, with the session.

## Phase 4: the queue

The app shows the queue of each deck: the files in their order, each with its material and its
time, and the total time. It can move a file and remove a file. It has no Start.

Deck (Go):

- `GET /api/plugin/printdeck/queue`: the files of the queue (`queue.Item`) and the total time.
- `POST /api/plugin/printdeck/queue/<id>/move` with `{"by": -1}`, and
  `DELETE /api/plugin/printdeck/queue/<id>`.
- A push message at the end of a print when the queue has files: "Next: Pod door · Black PLA ·
  45 min". The message opens the deck card. The deck screen has the question with Start.

Away from home, the queue of a project comes from the service (`ProjectView.Queue`). A project
sees only its own files.

## Order of the work

1. The deck: the app API on the resin deck, and the Bonjour TXT records (phase 1).
2. The app: the list of decks, the home screen, the Live Activity and the widgets (phase 1).
3. The service: the association file. The app: the sign-in and the cards away from home
   (phase 2).
4. The app: projects (phase 3).
5. The deck: the queue routes and the push message. The app: the queue (phase 4).
