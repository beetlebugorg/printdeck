# The GitHub App of Printdeck

The GitHub App lets the service start the slice run of a build: Slice on the plan page then
starts the workflow `printdeck.yml` of the repository. With no App, the build page shows the
command `gh workflow run` that starts the run by hand.

The App has one permission: it can start workflows. It reads no code, and it cannot push.

## Make the App

1. On GitHub, open Settings, Developer settings, GitHub Apps, New GitHub App. For an
   organization: the settings of the organization, Developer settings, GitHub Apps.
2. Fill in:
   - **GitHub App name:** for example `Printdeck beetlebug` (the name must be free on GitHub).
   - **Homepage URL:** `https://printdeck.beetlebug.org`
   - **Webhook:** clear Active. The service needs no webhook yet.
   - **Repository permissions:** Actions: Read and write. Metadata is Read-only, and GitHub sets
     it. Leave all other permissions at No access.
   - **Where can this GitHub App be installed?** Only on this account.
3. Tap Create GitHub App. The page shows the **App ID**: keep it.
4. Under Private keys, tap Generate a private key. GitHub downloads a `.pem` file.
5. Tap Install App in the menu, and install it on the account. Choose **Only select
   repositories**, and pick the repositories of the projects, for example `bittlex`.

## Give the App to the service

```
cd cloud
go run ./cmd/pwf github-app --id <App ID> --key ~/Downloads/<name>.private-key.pem
go run ./cmd/pwf deploy
```

`pwf github-app` keeps the App ID and the key in the secret `printdeck-cloud-github-app` of
Secrets Manager. `pwf deploy` gives the secret to the function. Then delete the `.pem` file from
the computer.

## How the service uses it

For each build, the service:

1. signs a JWT of the App, which works for 10 minutes;
2. finds the installation of the App on the repository;
3. gets a token of the installation for that one repository, with only the permission Actions;
4. starts `.github/workflows/printdeck.yml` on the default branch, with the input `build`.

The slice run then sends its run ID, and the build page links to the run on GitHub. When the App
is not on the repository, the build fails, and the build page says so.

## A new key

Generate a new private key on the page of the App, run `pwf github-app` and `pwf deploy` again,
then delete the old key on the page of the App.
