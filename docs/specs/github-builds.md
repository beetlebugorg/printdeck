# Builds from GitHub

A project has one GitHub repository. The repository holds the models and the printer profiles.
GitHub Actions slices the models. Each sliced file is a build of one commit. A deck prints a build
only after a person presses Start on the deck. Each print then links to its commit, and the
results go back to the pull request.

## Words

- **Plan.** The parts of one commit, before a slice: the models, and the parts that changed.
  The Action uploads a plan at each push to a pull request, a branch or a tag. A plan has a
  kind:
  - **Test:** from the head commit of a pull request.
  - **Main:** from a commit on the default branch.
  - **Release:** from a tag, for example `v1.2`.
- **Part.** One entry of `printdeck.yml`: one or more model files, a count, a printer, a profile
  and a material.
- **Plate.** The parts that print together in one print on one printer. The owner makes the
  plates of a plan before the slice.
- **Build.** The sliced plates of a plan, with a manifest: one G-code or resin file for each
  plate.
- **Print request.** A request to print one plate of a build on one printer. The deck of that
  printer shows it, and a person starts it or declines it.

## The workflow

1. A friend opens a pull request with a change to a model.
2. The Action uploads the plan of the commit: the models of all the parts, and the parts that
   changed. It slices nothing yet. The commit gets the status "printdeck: choose the parts",
   with a link to the plan page.
3. The owner opens the plan page. It suggests the parts that changed. The owner chooses the
   parts, and puts them on plates: each part alone, or parts together (see "The plates").
4. The owner taps Slice. The service starts the workflow of the repository with the plates
   (`workflow_dispatch`). The Action slices each plate and uploads the build.
5. The owner opens the preview: the slice of each plate in 3D. Approve sends the plate to the
   deck of its printer. Reject asks for a reason, and the reason goes to the pull request.
6. The deck asks for the material, and the owner presses Start.
7. The print ends. The service posts the result, the photos and the notes on the pull request.
8. The friend reads the comment, and changes the model in the same pull request. Steps 2 to 7
   repeat. The pull request shows each try with its photos.
9. The owner merges the pull request and adds a tag. The release plan suggests all the parts.
10. The owner prints the release build and marks the prints Delivered. The page then shows which
    release each person got.

## The plan page

`/projects/<slug>/plans/<id>`, for the owner. The members see it with no keys.

- Each part of `printdeck.yml` has a row: its name, its printer, its profile and material, its
  count, a 3D view of its model, and the label "Changed" when it changed. A part changed when
  one of these changed in the pull request against its base branch (or since the last plan of a
  branch): its files, the files of its `generate:` source, its entry in `printdeck.yml`, or the
  folder of its profile. A release plan marks no part, and suggests all of them.
- A check box chooses each part. The parts that changed are checked at first.
- The count of a part can change for this build, for example 2 feet in place of 4 for a test.
- The plates (see "The plates") show under the parts, each with the printer and a 3D view of the
  layout on the bed.
- Slice starts the slice of the plates. The page then waits for the build, and opens the preview.
- The owner can slice the same plan again with other plates: each slice makes a new build of the
  plan.

## The plates

Before the slice, the owner chooses how the parts print: each part on its own plate, or parts
together on one plate.

- Only parts with the same printer, profile and material can share a plate. The page shows the
  other parts as a separate plate.
- The suggestion: the checked parts with the same printer, profile and material on one plate,
  when they fit on the bed. A part that does not fit with the others gets its own plate.
- The owner can move a part to another plate or to a new plate, and join or split plates.
  `printdeck.yml` can ask for a part to print alone (`alone: true`), for example a tall part.
- The page checks that each plate fits on the bed of its printer, with the room for the brim or
  the skirt. A plate that does not fit shows how many copies fit.
- The slice of a plate has the parts at the places of the layout. A part with one file and one
  copy on its own plate goes to the middle of the bed, as the Cura app places it.

## The approval

The Action ends after the upload of the build. The build then waits on the service, and the
workflow on GitHub shows the wait as the commit status `printdeck/approval`.

- The preview page: `/projects/<slug>/builds/<id>`. It shows each plate in a 3D view, on the bed
  of its printer, with the facts of the slice: the time, the grams or the ml, the layers, the
  layer height, the supports, and the profile with its version.
  - **FDM:** the moves of the G-code as lines (the `path.bin` form of the printer pages spec),
    colored by the kind of move: wall, infill, support, skirt. A slider shows the layers up to a
    height, to see the inside and the first layer.
  - **Resin:** the share of resin in each cell (`part.bin`, PDC1), as the share page shows a
    resin part now, with the supports. A slider cuts the part at a height, and the layer image
    of that height shows next to it.
- **Approve** on a plate sends a print request to the deck of its printer (see "The deck"). The
  status of the commit becomes success when all the plates of the build have an approval.
- **Reject** on a plate asks for a reason. The status becomes failure, and the reason goes to the
  pull request as a comment.
- A new plan of the same pull request replaces the waiting plates of the older builds: they show
  "Replaced".
- The Action makes `part.bin` and `path.bin` with the build, so the page needs no work on the
  service to show the preview.

## The two runs of the Action

The workflow of the repository has two jobs. Both use `beetlebugorg/printdeck-action`.

1. **plan** (on `push`, `pull_request` and tags): reads `printdeck.yml`, runs `generate:` when
   the part needs it, finds the parts that changed, and uploads the plan with the models (STL
   or 3MF, for the 3D views). It takes about a minute.
2. **slice** (on `workflow_dispatch`, with the input `build`: the ID of a build with its plates):
   checks out the commit of the plan, gets the plates from the service, slices them, and uploads
   the files of the build.

The service starts the slice run with the GitHub App (`actions: write` on the repository). The
run uses the same OIDC token check as the plan run. The input has only the ID of the build: the
plates come from the service, so a person who starts a run by hand cannot change them.

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
- `curaslice.py` of printdeck-action works out the settings as the Cura app does. A slice of the cover of
  bittlex matches the slice of the Cura app: the same time, filament, size and layers.

A part can have `variants:`, the qualities of the part, for example a fine and a standard print.
Each variant has a name, and can change the files, the profile, the prebuilt file, the material,
the count and `alone`; its `settings` add to the settings of the part. The model of a part can
change for each quality: the generate step then writes a file for each variant. Each variant is
a part of its own in the plan, with its own mark "Changed", and the plates, the G-code and the
prints name the variant.

A part can change settings of its profile with `settings:` (Cura setting names for FDM), for
example `support_structure: tree` or `brim_line_count: 14`. A 3MF file keeps the per-mesh
settings of Cura, for example a support blocker (`cura:anti_overhang_mesh`): the Action passes
each mesh of the 3MF to CuraEngine with its settings.

A part with more than one file has all of them on one plate. `count` puts that many copies of
each file on the plate (1 when it is not there). The slicer arranges them. When the copies do not
fit on one plate, the Action fails with the count that fits.

## The Action

The repository uses the action `beetlebugorg/printdeck-action@v1` (https://github.com/beetlebugorg/printdeck-action). Its README has the
workflow file.

- **FDM:** CuraEngine 5.13 with `curaslice.py` and a profile of `profiles/` (see "The
  profiles"). A part can ask for `slicer/AdaptiveSpeed.py` after the slice with
  `adaptive_speed: true`. The Cura app of the owner does not run it now, so it is off by default.
- **Resin:** PrusaSlicer in SLA mode makes an SL1 file with supports. UVtools converts it to
  `.cbddlp` for the Phenom (3840 x 2160, 0.072 mm pixels, the projector mirror).
  - A part can also have a `prebuilt:` file in place of `profile:`: a `.cbddlp` that a person
    made with ChiTuBox and committed. The Action then only checks its header. This keeps the
    supports of ChiTuBox for parts that need them.
- The plan run uploads the plan; the slice run slices the plates of a build and uploads the
  files (see "The two runs of the Action" and "The upload").

The plan, `plan.json`:

- The repository, the commit, the ref, the pull request number or the tag.
- For each part: the name, the files, the count, the printer, the profile with its version, the
  material, `alone`, the size of its model on the bed, and `changed`.

The manifest of a build, `build.json`:

- The plan, and the plates that the owner chose.
- For each plate: the printer, the profile with its version, the material, the parts with their
  counts, the file name, its size and SHA-256, the estimate of the time, the grams or the ml, the
  layers, and the version of the slicer.

### The upload

The Action needs no secret. It asks GitHub for an OIDC token with the audience
`printdeck.beetlebug.org`, and sends it with the upload.

1. The plan run sends `POST /api/v1/plans` with the token and the plan. The slice run sends
   `GET /api/v1/builds/<id>` for the plates, and `POST /api/v1/builds/<id>/files` with the
   manifest of the files.
2. The API checks the token: the signature against the keys of GitHub, the audience, the expiry,
   and the claim `repository`. The repository must be the repository of the project. For a
   slice run, the commit of the token must be the commit of the plan.
3. The API replies with grants for the files, as for the files of a print now.
4. The Action uploads the files, then sends `POST /api/v1/plans/<id>/done` or
   `POST /api/v1/builds/<id>/done`.

Limits: 200 MB for each file and 1 GB for each build. The service keeps test builds for 30
days after the pull request closes, and keeps main and release builds.

## The service

- `x/<project>/project.json` gets `repo` (`owner/name`). The owner sets it on the project page.
- `x/<project>/plans/<id>.json` is a plan, and `x/<project>/plans/<id>/<file>` its models.
- `x/<project>/builds/<id>.json` is a build: its plan, its plates, and the state of each plate.
- `x/<project>/builds/<id>/<file>` are the sliced files. The decks read them with a grant.
- `x/<project>/requests/<id>.json` is a print request: the build, the part, the printer, the
  person who asked, the time, and the state (waiting, declined, started, ended).

## The project page

- A "Builds" section, grouped by pull request and by tag, newest first. Each plan shows its
  commit, the parts that changed, and its builds. Each build shows its plates and the prints of
  each plate.
- Each plate has the key Preview, and its state: waiting for an approval, approved, rejected,
  printed. Only the owner can choose plates, slice, approve or reject. The members see the plans,
  the previews and the states.
- The print tiles show their build: "PR #12 · 3f2a9c1" or "Release v1.2".
- The print page shows the pull request with its comments, and the link to the commit.

### The PDFs of a project

`docs` in printdeck.yml lists the PDFs of the project: a build guide or a spec. Each entry has a
`name` and a `file`. The plan run uploads each PDF (at most 10, each at most 50 MB) into the folder
of the plan as `doc<n>.pdf`, with a PNG of its first page (`doc<n>.png`, 600 pixels wide, made by
`pdftoppm`). The plan record keeps the name, the path, the size, the pages and the SHA-256 of each
PDF. `POST plans/<id>/done` needs each PDF. A PDF with no picture gets an icon on the page.

The overview of the project has the card Documents: the PDFs of the newest ready plan of the
default branch, else of the newest ready plan of a tag. A plan of a pull request or of a fork
does not change the card. Each PDF has a tile with the picture of its first page, its pages and its
size, Open PDF (a signed link), and a link to the file on GitHub at the commit of the plan. The
first PDF has the largest tile.

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

- A request of the Action needs an OIDC token of the repository of the project, for the
  audience of the service. A fork has another repository claim, and a pull request from a fork
  gets no token at all.
- Each token works for one request: the API records the ID (`jti`) of each token in
  `oidc/<sha256>`, with a write that fails when the record exists. The Action asks GitHub for a
  new token for each request. A lifecycle rule deletes the records after a day.
- Only the reviewed workflow slices: each request on a build needs a token of a job of
  `.github/workflows/printdeck.yml` on the default branch (`job_workflow_ref`). A copy of the
  workflow on a branch or in a pull request can send a plan, which is only a proposal, but it
  cannot fill a build.
- The deck checks the SHA-256 of each file before it sends it to the printer.
- A print request never starts a print. A person on the deck starts it.
- The comments on GitHub have no data of the passkeys, no invite links and no share keys.

## Phases

1. The GitHub App, the repository link, the plan run, the plan page with the plates, the slice
   run, the builds on the project page, and the build of a print record.
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
    files: [bittle_pi_addon/stl/plates/resin_phenom_plate.stl]
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
