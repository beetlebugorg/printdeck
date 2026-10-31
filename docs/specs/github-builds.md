# Builds from GitHub

A project has one GitHub repository. The repository holds the models and the printer profiles.
GitHub Actions slices the models. Each sliced file is a build of one commit. A deck prints a build
only after a person presses Start on the deck. Each print then links to its commit, and the
results go back to the pull request.

## Words

- **Build.** The sliced files of one commit, with a manifest. A build has a kind:
  - **Test build:** from the head commit of a pull request.
  - **Main build:** from a commit on the default branch.
  - **Release build:** from a tag, for example `v1.2`.
- **Part.** One file to print in a build: one plate of the FDM printer or one plate of the resin
  printer.
- **Print request.** A request to print one part of a build on one printer. The deck of that
  printer shows it, and a person starts it or declines it.

## The workflow

1. A friend opens a pull request with a change to a model.
2. The Action slices the parts that changed. The project page shows the test build.
3. The owner taps Print on a part. The deck asks for the material, and the owner presses Start.
4. The print ends. The service posts the result, the photos and the notes on the pull request.
5. The friend reads the comment, and changes the model in the same pull request. Steps 2 to 4
   repeat. The pull request shows each try with its photos.
6. The owner merges the pull request and adds a tag. The Action makes the release build.
7. The owner prints the release build and marks the prints Delivered. The page then shows which
   release each person got.

## The repository

```
printdeck.yml          the parts, the printers and the materials
models/                STL or 3MF files (or the source, see "Sources")
profiles/taz/          the Cura definition and the profile of the TAZ Workhorse
profiles/phenom/       the slicer profile of the Peopoly Phenom
.github/workflows/printdeck.yml
```

`printdeck.yml`:

```yaml
project: robot            # the slug of the Printdeck project
parts:
  - name: Shoulder pieces
    files: [models/shoulder-left.stl, models/shoulder-right.stl]
    printer: phenom
    material: Grey resin
    profile: profiles/phenom/standard-0.05.ini
  - name: Base plate
    files: [models/base.stl]
    printer: taz
    material: PLA, black
    profile: profiles/taz/pla-0.2.cfg
```

A part with more than one file has all of them on one plate. The slicer arranges them.

## The Action

The repository uses the action `printdeck/slice` from this repository (`actions/slice/`). It runs
in a container with the slicers.

- **FDM:** CuraEngine with the Cura definition of the TAZ Workhorse and the profile of the part.
  The Action then runs `slicer/AdaptiveSpeed.py` on the G-code, as the deck profile does now.
- **Resin:** PrusaSlicer in SLA mode makes an SL1 file with supports. UVtools converts it to
  `.cbddlp` for the Phenom (3840 x 2160, 0.072 mm pixels, the projector mirror).
  - A part can also have a `prebuilt:` file in place of `profile:`: a `.cbddlp` that a person
    made with ChiTuBox and committed. The Action then only checks its header. This keeps the
    supports of ChiTuBox for parts that need them.
- The Action slices only the parts whose files or profiles changed since the last build of the
  same branch. A tag slices all the parts.
- The Action uploads the manifest and the files to the service (see "The upload").

The manifest, `build.json`:

- The repository, the commit, the ref, the pull request number or the tag.
- For each part: the name, the printer, the material, the file name, its size and SHA-256, the
  estimate of the time, the grams or the ml, the layers, and the versions of the slicers.

### The upload

The Action needs no secret. It asks GitHub for an OIDC token with the audience
`printdeck.beetlebug.org`, and sends it with the upload.

1. `POST /api/v1/builds` with the token and the manifest.
2. The API checks the token: the signature against the keys of GitHub, the audience, the expiry,
   and the claim `repository`. The repository must be the repository of the project.
3. The API replies with grants for the files, as for the files of a print now.
4. The Action uploads the files, then sends `POST /api/v1/builds/<id>/done`.

Limits: 200 MB for each file and 1 GB for each build. The service keeps test builds for 30
days after the pull request closes, and keeps main and release builds.

## The service

- `x/<project>/project.json` gets `repo` (`owner/name`). The owner sets it on the project page.
- `x/<project>/builds/<id>.json` is the manifest, with the state of each part.
- `x/<project>/builds/<id>/<file>` are the sliced files. The decks read them with a grant.
- `x/<project>/requests/<id>.json` is a print request: the build, the part, the printer, the
  person who asked, the time, and the state (waiting, declined, started, ended).

## The project page

- A "Builds" section, grouped by pull request and by tag, newest first. Each build shows its
  commit, its parts, and the prints of each part.
- Each part has a Print key. Only the owner sees it at first (see "Questions").
- The print tiles show their build: "PR #12 · 3f2a9c1" or "Release v1.2".
- The print page shows the pull request with its comments, and the link to the commit.

## The deck

1. The deck asks the service for print requests of its printers, each minute, and at once when
   the live link says so.
2. A print request shows on the deck as a panel. It never starts a print by itself.
   - The project, the build, the part, and the commit.
   - The material to load: "Load PLA, black: about 32 g", or "Fill the vat with Grey resin:
     about 41 ml". The deck compares this with the roll in the printer. It warns when the
     material is not the same, or when the roll has too little left.
   - For resin: "Is the build plate clean and in place?"
   - The keys Start and Not now.
3. Start downloads the file. The deck checks its size and its SHA-256 with the manifest.
4. FDM: the deck uploads the file to Klipper and starts it. Resin: the deck sends the file to
   the Phenom with the relay upload, then starts it. The rule of `resin-start-safety` applies:
   only a fresh press on the deck starts a resin print.
5. The print record gets the build, the commit, the ref and the part. The deck sends it to the
   project at the end, as now.

## Back to GitHub

The service uses a GitHub App, installed on the repository. Its private key is a secret in
Secrets Manager, as the key of the signed cookies.

- At the end of a print of a test build, the service posts a comment on the pull request: the
  result, the time, the material, the picture of the model, the photos, the notes, and a link to
  the print page. A later change of the notes or a new photo updates the same comment.
- The service sets a commit status `printdeck/print` on the commit: pending while it prints,
  then success or failure.
- A release build gets the same comment on its GitHub release.
- The webhook of the App tells the service about pull requests, comments, merges and tags. The
  service checks the HMAC of each delivery with the secret of the webhook.

## Sources

The Action can make the STL files first: OpenSCAD or CadQuery files in `models/`, with a
`source:` entry in the part. The STL files then never go into the repository.

## Security

- A build upload needs an OIDC token of the repository of the project. A fork has another
  repository claim, so a fork cannot upload. Builds of pull requests from forks run in the
  repository with `pull_request_target` only when the owner allows it.
- The deck checks the SHA-256 of each file before it sends it to the printer.
- A print request never starts a print. A person on the deck starts it.
- The comments on GitHub have no data of the passkeys, no invite links and no share keys.

## Phases

1. The repository link, the OIDC upload, the builds on the project page, and the build of a print
   record (a print that starts on the deck with a file of a build).
2. The print requests and the panel on the deck, with the material check.
3. The GitHub App: the comments, the commit status, and the pull request on the print page.
4. Resin slicing in the Action with PrusaSlicer and UVtools. Until then, resin parts use
   `prebuilt:` files from ChiTuBox.
5. The sources: OpenSCAD and CadQuery.

## Questions

- Are the repositories public or private? Private repositories need the GitHub App from phase 1.
- Who can tap Print: only the owner, or the members too?
- Does a part need a count, for example 4 copies on one plate?
- Resin: are the supports of PrusaSlicer good enough for the Phenom, or do the parts stay with
  ChiTuBox files?
