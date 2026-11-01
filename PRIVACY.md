# Privacy policy

Last updated: November 1, 2026

This policy tells you what data Printdeck keeps, where it keeps the data, and how long. It also
tells you how to delete your data.

## Who runs the service

Jeremy Collins runs the Printdeck cloud service at https://printdeck.beetlebug.org. Printdeck is
free software under the GNU AGPL, version 3. One person makes it and runs it.

The deck software is self-hosted. A person who runs a copy of the cloud service is the operator
of that copy, and this policy does not apply to it.

You can check the claims of this policy:

- The source code: https://github.com/beetlebugorg/printdeck.
- The architecture document: https://beetlebug.org/printdeck/architecture/. The chapters
  [Identity and trust](https://beetlebug.org/printdeck/architecture/identity-and-trust/),
  [Why it is secure](https://beetlebug.org/printdeck/architecture/why-it-is-secure/) and
  [The data of a print](https://beetlebug.org/printdeck/architecture/the-data-of-a-print/) tell
  how the parts below work.

## The parts of Printdeck

- **The deck**: a small computer with a touch screen next to a 3D printer. It works on your
  local network.
- **The cloud service**: the web pages at https://printdeck.beetlebug.org, the share links of
  prints, and the projects that the owner shares with friends.
- **The iPhone app and the Apple Watch app.**
- **The documentation site** at https://beetlebug.org/printdeck/.

The owner is the person who connects a deck to the cloud service. A member is a friend who joins
a project of the owner with an invite.

## What data we keep

### The deck

The deck keeps its data on its own SD card. The data does not go to the cloud service until the
owner connects the deck to it. The deck keeps:

- The history of the last 100 prints: the file, the result, the time, the filament and your
  notes.
- The time lapse videos, if you turn them on. The deck keeps the newest 3.
- The name and the key of your Wi-Fi network.
- The phones that you paired for notifications: the device token, the keys of the messages and
  the name of the phone.
- The API keys of the slicers that you connected.
- The key of the deck for the cloud service.

The logs of the deck are in its memory only. A restart deletes them.

The deck connects to these servers with no action of the owner:

- GitHub, once a day, to look for a new version of Printdeck. GitHub gets the IP address of your
  network. The deck downloads an update only when you tap Install.
- A time server, to set the clock. The deck uses the time server of your network. If your network
  gives none, the deck uses the time servers of Google.
- A DNS server, to find the addresses of the servers. The deck uses the DNS server of your
  network. If your network gives none, the deck uses the DNS servers of Cloudflare and Google.

The deck sends data to other servers only when the owner turns on a feature:

- **The cloud service**: when the owner connects the deck with a code.
- **ntfy**: when the owner sets an ntfy topic. The deck sends the notifications as plain text to
  ntfy.sh, or to the ntfy server that the owner sets.
- **Push notifications to the iPhone app**: when the owner pairs a phone. See below.

### The cloud service

The cloud service keeps no passwords and no email addresses. You sign in with a passkey.

When the owner connects a deck, the service keeps:

- **The owner**: a first name, and the passkeys of the owner. A passkey record has the public
  key, the type of the device (for example "iPhone") and the date.
- **The decks**: the name of each deck, its public key, and the date it connected.
- **The printers**: the name, the model and the type of each printer, and what it prints now.

When the owner makes a share link of a print, the deck sends to the service, until the print
ends:

- The name of the print and of the printer.
- A camera picture every 5 seconds, the frames of the time lapse, and a picture of the model.
- The state of the print: the progress, the layers and the times.
- The file of the print, for the 3D view.
- The photos that the owner adds, and the note of "Ready to pick up".

When the owner uses projects, the service keeps:

- **The project**: the name, a line about it, and the notes of the people of the project, with
  the first name of the last person who changed them.
- **The members**: the first name of each member, their passkeys, the date they joined, and their
  GitHub username if the owner sets one.
- **The invites**: the first name that the owner gives the invite.
- **The prints**: the name and the file of each print, the result, the times, the material, the
  grams, the roll, the printer and the filament settings. Also the picture of the model, the time
  lapse frames, the file of the print and the notes.
- **The photos**: the photos that the owner and the members add, with the first name of the
  person who added each. The service makes a new copy of each photo and removes all of its
  metadata, for example the GPS position and the camera model. It deletes the photo as uploaded.
- **The queue**: the names of the files of the project that wait on a printer.
- **The plans and the builds from GitHub**, if the owner links a GitHub repository: the name of
  the repository, the commits, the branches, the pull requests, the models, the sliced files and
  the PDFs. A plan from the fork of a member has the name of the fork and the GitHub username of
  the member.

The service writes logs. A log line has the type of request, the route with no IDs, the time it
took and the count of storage calls. An error line can have the name of a GitHub repository and
the number of a pull request. The service does not write your IP address to its logs.

### The iPhone app and the Apple Watch app

The apps connect to the deck on your local network. The apps keep the address and the API key of
each deck, and your session of the cloud service, in the iPhone keychain. The deck key never goes
to the cloud service.

For push notifications, the app sends its device token to the push relay of the cloud service.
The relay keeps no list of devices. The deck encrypts each message for your phone, and only the
app can read it. The relay and Apple see the device token and the encrypted message only.

The app sends the photos that you take for a project to the cloud service.

### The documentation site

GitHub Pages hosts the documentation site. GitHub gets the IP address of each visitor.

## Why we keep the data

We keep the data only to run Printdeck for you:

- To show your prints, projects and photos to you and to the people you choose.
- To sign you in.
- To send notifications to your phone.
- To slice and comment on the models of a linked GitHub repository.
- To find and fix errors (the logs).

We do not sell data. We show no ads. We do not track you. The service has no analytics and no
trackers.

## Who can see the data

- **The owner** sees all the projects, prints, photos and members.
- **A member** sees the projects that they joined: the prints, the photos, the notes and the
  first names of the other members.
- **A share link** (`/s/<id>`) is public: any person with the link sees the print. The link has a
  random ID. The link stops 24 hours after the print ends.
- **A public address** (`/p/<name>`) is public: any person with the address sees the project. The
  page shows the name of the project, the notes, the first name of the owner, the prints, the
  time lapses, the photos and the first names of the people who added them. It does not show the
  names of the files or the rolls. The owner turns the public address on and off.
- **The pictures in the comments on GitHub pull requests** are public files with random names.
  Any person who can see the pull request can see them.

The pages of the cloud service ask search engines not to show them in search results.

## Third parties

- **Amazon Web Services** hosts the cloud service in the United States (the us-east-1 region).
  AWS keeps the data, runs the code and delivers the pages.
- **Apple** delivers the push notifications (APNs). Apple gets the device token and the encrypted
  message.
- **GitHub** gets the IP address of the deck at each update check. If the owner links a GitHub
  repository, the service reads it and writes comments on its pull requests, with pictures of the
  models, the prints and the photos. GitHub also hosts the documentation site.
- **Google Fonts** delivers the fonts of the pages of the cloud service and of the documentation
  site. Your browser sends your IP address to Google when it gets the fonts.
- **Google and Cloudflare** get time and DNS requests from the deck when your network gives no
  servers.
- **ntfy** gets the notifications of the deck, if the owner sets an ntfy topic.

## How long we keep the data

| Data | How long |
|---|---|
| A share link and its files | 24 hours after the print ends, or after "Ready to pick up". 3 days if the print never ends. |
| A share link of a print in a project | Its files go at the same time. The link then opens the page of the print. |
| An invite | Until it is used or removed, at most 7 days. |
| A code to connect a deck | 1 hour. |
| A code for the owner passkey | 10 minutes. |
| A photo as uploaded | Until the service cleans it, at most 1 day. |
| A sign-in | 14 days. Sign out removes it from your browser. |
| The logs | 3 days. |
| The backups | 7 days. |
| Projects, prints, photos, plans and builds | Until the owner deletes them. |
| A member | Until the owner removes the member, or the member deletes their account. |
| A passkey | Until the owner removes it or its member, or the member deletes their account. |

The service makes a backup of all its data each day, and keeps each backup 7 days. A deleted
file also stays as an old version for 1 to 2 days. After a delete, a copy of your data can stay
in the backups and the old versions for up to 9 days. Then it is gone.

The network of AWS (CloudFront) keeps copies of the public files near the visitors. A copy of a
deleted public file can stay there for a time after the delete.

## Cookies and browser storage

- The pages keep your sign-in in the local storage of your browser. Sign out removes it.
- When you see a project, the service sets three cookies (`CloudFront-Policy`,
  `CloudFront-Signature` and `CloudFront-Key-Pair-Id`). They let your browser get the files of
  that project. They work for 1 hour.

The service sets no other cookies.

## Your choices, and how to delete your data

The owner deletes data in the web app or on the deck:

- Delete a photo, a print or a project. The delete of a project removes its prints, photos,
  invites, members, plans, builds, public address, the share links of its prints that ended,
  and the pictures of its comments on GitHub. The delete of a print removes its record, the picture of its model and its time
  lapse. Its photos stay in the project.
- Remove a member. "Remove" ends the access of the member to the project. "Delete data" also
  deletes the photos of the member, and their GitHub username.
- Turn off the public address of a project.
- Stop a share link on the deck. Stop deletes the share link and its files at once.
- Remove a passkey. The service keeps the last passkey of the owner, because the service needs an
  owner.
- Disconnect a deck in Settings, Cloud.

A member deletes their account in the web app: sign in, then tap "Delete my account" on the page
of the projects. The service deletes the passkeys and the photos of the member, and removes the
member from each project.

When a member is deleted, the notes stay, because other people wrote them too. The name of the
member on them changes to "A former member". The prints and the history of the project stay.

You can delete the data on your deck. Remove the deck from the cloud service, then write a new
Printdeck OS image to the SD card.

If you do not have an account, or you want help, write to privacy@beetlebug.org.

## Children

Printdeck is not for children under 13. We do not knowingly keep data of a child under 13. If you
think that we have such data, write to privacy@beetlebug.org, and we delete it.

## Security

- You sign in with a passkey. The service keeps no passwords.
- A deck signs each request with a key of its own.
- The service removes the metadata of each photo.
- All the data of the service is in one private storage bucket, encrypted on disk. The pages and
  the API use HTTPS only.
- The files of a project need a signed cookie of that project.
- The push messages are encrypted from the deck to your phone.

## Changes to this policy

We change this policy when Printdeck changes what it keeps. The date at the top tells you the
date of the last change. The history of each change is on GitHub:
https://github.com/beetlebugorg/printdeck/commits/main/PRIVACY.md.

## Contact

Write to privacy@beetlebug.org.
