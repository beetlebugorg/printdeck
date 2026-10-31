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
2. The Action slices the parts that changed. The project page shows the test build, and the
   commit gets the status "printdeck/approval: pending", with a link to the preview.
3. The owner opens the preview: the slice of each part in 3D. Approve sends the part to the deck
   of its printer. Reject asks for a reason, and the reason goes to the pull request.
4. The deck asks for the material, and the owner presses Start.
5. The print ends. The service posts the result, the photos and the notes on the pull request.
6. The friend reads the comment, and changes the model in the same pull request. Steps 2 to 5
   repeat. The pull request shows each try with its photos.
7. The owner merges the pull request and adds a tag. The Action makes the release build, and it
   waits for an approval too.
8. The owner prints the release build and marks the prints Delivered. The page then shows which
   release each person got.

## The approval

The Action ends after the upload. The build then waits on the service, and the workflow on
GitHub shows the wait as the commit status `printdeck/approval`.

- The preview page: `/projects/<slug>/builds/<id>`. It shows each part in a 3D view, on the plate
  of its printer, with the facts of the slice: the time, the grams or the ml, the layers, the
  layer height, the supports, and the profile.
  - **FDM:** the moves of the G-code as lines (the `path.bin` form of the printer pages spec),
    colored by the kind of move: wall, infill, support, skirt. A slider shows the layers up to a
    height, to see the inside and the first layer.
  - **Resin:** the share of resin in each cell (`part.bin`, PDC1), as the share page shows a
    resin part now, with the supports. A slider cuts the part at a height, and the layer image
    of that height shows next to it.
- **Approve** on a part sends a print request to the deck of its printer (see "The deck"). The
  status of the commit becomes success when all the parts of the build have an approval.
- **Reject** on a part asks for a reason. The status becomes failure, and the reason goes to the
  pull request as a comment. The next commit makes a new build.
- A new build of the same pull request replaces the approval: the older build shows "Replaced".
- The Action makes `part.bin` and `path.bin` with the build, so the page needs no work on the
  service to show the preview.

## The parts of a print

A round of tests can print only the parts that change, or all the parts. The person who sends a
build to the printers chooses, and the page starts with a suggestion.

- Each build has all the parts, sliced. The Action marks each part that changed in the pull
  request against its base branch: its files, the files of its `generate:` source, its entry in
  `printdeck.yml`, or the folder of its profile. A build of a branch compares with the last
  build of the branch, and a release build marks no part.
- The build page lists all the parts with a check box each. The parts that changed are checked
  at first, with the label "Changed". The owner checks or unchecks parts, and taps Approve
  the checked parts. Each part keeps its own state.
- So in a round of tests of the pod and the shell, the owner can print only the pod. A full
  test checks all the parts.
- Reject still works for one part, with a reason for the pull request.

## The repository

```
printdeck.yml               the parts, the printers and the materials
models/                     STL or 3MF files (or the source, see "Sources")
profiles/taz-workhorse-se/  the Cura stack of the TAZ Workhorse (from printdeck/profiles)
.github/workflows/printdeck.yml
```

`printdeck.yml`:

```yaml
project: robot            # the slug of the Printdeck project
parts:
  - name: Feet
    files: [models/foot.stl]
    count: 4                # 4 copies on one plate
    printer: phenom
    material: Grey resin
    prebuilt: phenom/feet.cbddlp
  - name: Shoulder pieces
    files: [models/shoulder-left.stl, models/shoulder-right.stl]
    printer: phenom
    material: Grey resin
    prebuilt: phenom/shoulders.cbddlp
  - name: Base plate
    files: [models/base.stl]
    printer: taz
    material: roll-4                         # a material of the profile folder
    profile: taz-workhorse-se/pla-0.20-standard
```

## The profiles

The profiles come from the Cura app of the owner. `slicer/export_cura_profile.py` copies them into
`profiles/` of the printdeck repository (see `profiles/README.md`). A repository that prints
copies the folder of each profile that it uses into its own `profiles/`. So a build depends only
on the commit of the repository, and a pull request can change a profile.

- A part names its profile as `<folder>/<profile>`, for example `taz-workhorse-se/pla-0.10-fine`,
  and its material as a material of that folder, for example `roll-4`.
- Each profile has a version: the first 12 hex digits of the SHA-256 of its folder. The manifest
  of a build has it for each part, and each G-code has it in the line `;Printdeck profile:`. The
  build page and the print page show the profile and the version of each print.
- `slicer/curaslice.py` works out the settings as the Cura app does. A slice of the cover of
  bittlex matches the slice of the Cura app: the same time, filament, size and layers.

A part can change settings of its profile with `settings:` (Cura setting names for FDM), for
example `support_structure: tree` or `brim_line_count: 14`. A 3MF file keeps the per-mesh
settings of Cura, for example a support blocker (`cura:anti_overhang_mesh`): the Action passes
each mesh of the 3MF to CuraEngine with its settings.

A part with more than one file has all of them on one plate. `count` puts that many copies of
each file on the plate (1 when it is not there). The slicer arranges them. When the copies do not
fit on one plate, the Action fails with the count that fits.

## The Action

The repository uses the action `printdeck/slice` from this repository (`actions/slice/`). It runs
in a container with the slicers.

- **FDM:** CuraEngine 5.13 with `slicer/curaslice.py` and a profile of `profiles/` (see "The
  profiles"). A part can ask for `slicer/AdaptiveSpeed.py` after the slice with
  `adaptive_speed: true`. The Cura app of the owner does not run it now, so it is off by default.
- **Resin:** PrusaSlicer in SLA mode makes an SL1 file with supports. UVtools converts it to
  `.cbddlp` for the Phenom (3840 x 2160, 0.072 mm pixels, the projector mirror).
  - A part can also have a `prebuilt:` file in place of `profile:`: a `.cbddlp` that a person
    made with ChiTuBox and committed. The Action then only checks its header. This keeps the
    supports of ChiTuBox for parts that need them.
- The Action slices all the parts of `printdeck.yml`, and marks the parts that changed (see
  "The parts of a print").
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
- Each part has the key Preview, and its state: waiting for an approval, approved, rejected,
  printed. Only the owner can approve or reject. The members see the preview and the state.
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

A repository can make its STL files with a script, for example build123d in Python. The entry
`generate:` of `printdeck.yml` names the command and the files that it writes:

```yaml
generate:
  python: "3.12"
  install: pip install -r requirements.txt
  run: python bittle_pi_addon/pod.py
  check: true    # fail when the files differ from the committed files
```

With `check: true`, the committed files stay the files that print, and the build fails when a
person changed the source and did not commit the new files. Without `check`, the Action prints
the files of the script.

## Security

- A build upload needs an OIDC token of the repository of the project. A fork has another
  repository claim, so a fork cannot upload. Builds of pull requests from forks run in the
  repository with `pull_request_target` only when the owner allows it.
- The deck checks the SHA-256 of each file before it sends it to the printer.
- A print request never starts a print. A person on the deck starts it.
- The comments on GitHub have no data of the passkeys, no invite links and no share keys.

## Phases

1. The GitHub App, the repository link, the OIDC upload, the builds on the project page, and the
   build of a print record (a print that starts on the deck with a file of a build).
2. The preview page and the approval, the print requests, and the panel on the deck with the
   material check.
3. The GitHub App: the comments, the commit status, and the pull request on the print page.
4. Resin slicing in the Action with PrusaSlicer and UVtools. Until then, resin parts use
   `prebuilt:` files from ChiTuBox.
5. The sources: OpenSCAD and CadQuery.

## The first repository: bittlex

`beetlebugorg/bittlex` is public, with the default branch `main`. Python scripts with build123d
and manifold3d make the parts, and the STL, 3MF and STEP files are in the repository. Its old Cura
profile is `cura/Bittle_X.curaprofile` (0.20 mm layers, no supports, a 6-line brim). The builds
use the profiles of `printdeck/profiles/taz-workhorse-se` in its place. The repository has one workflow now: `pages.yml` builds the
GitHub Pages site. A draft of its `printdeck.yml`:

```yaml
project: bittlex
generate:
  python: "3.12"
  install: pip install build123d manifold3d
  run: python bittle_pi_addon/pod.py && python bittle_platform/build.py
  check: true
parts:
  - name: Pod body (screw, rev 0.26)
    files: [bittle_pi_addon/stl/print/pod_body_screw.3mf]
    printer: taz
    material: roll-4
    profile: taz-workhorse-se/pla-0.10-fine
  - name: Pod panel (screw)
    files: [bittle_pi_addon/stl/print/pod_panel_screw.stl]
    printer: taz
    material: roll-3
    profile: taz-workhorse-se/pla-0.10-fine
  - name: Cover (screw)
    files: [bittle_pi_addon/stl/print/cover_screw.stl]
    printer: taz
    material: roll-4
    profile: taz-workhorse-se/pla-0.10-fine
  - name: Pod door
    files: [bittle_pi_addon/stl/print/pod_door.stl]
    printer: taz
    material: roll-4
    profile: taz-workhorse-se/pla-0.20-standard
  - name: Platform cover
    files: [bittle_platform/stl/print/cover_pegs.stl]
    printer: taz
    material: roll-4
    profile: taz-workhorse-se/pla-0.20-standard
    settings: {support_enable: true, support_structure: tree, adhesion_type: brim, brim_line_count: 14}
  - name: Starter plate
    files: [bittle_platform/stl/print/starter_plate.stl]
    printer: taz
    material: roll-4
    profile: taz-workhorse-se/pla-0.20-standard
  - name: Resin plate
    files: [bittle_pi_addon/stl/plates/resin_phenom.stl]
    printer: phenom
    material: Grey resin
    prebuilt: phenom/resin_plate.cbddlp   # from ChiTuBox until the resin slicing works
```

Open points for bittlex:

- The file names and the plate file of `plates.json` need a check against the repository.
- The heavy build (wire settling, STEP export) takes minutes. The Action runs only the parts
  of the build that write the printed files, or it skips `check` on the STEP files.
- The resin plate on the Mac is out of date. A new ChiTuBox slice of the screw version goes in
  the repository as the `prebuilt` file.

## Decisions

- The repositories can be public or private. So the GitHub App comes in phase 1: the service
  reads private repositories with the token of the App installation.
- Only the owner approves or rejects a build.
- A part can have a count of copies on one plate.
- The first repository is the bittlex repository.

## Questions

- Resin: are the supports of PrusaSlicer good enough for the Phenom, or do the parts stay with
  ChiTuBox files?
