# The zslice G-code viewer

The build pages of the cloud show the G-code of a plate with this viewer (../zsview.js).

These files are the output of `zig build web-view -Doptimize=ReleaseSmall` of zslice
(github.com/beetlebugorg/zslice) at commit 139e6eae089d268afc09e51a8e5cbd5ad980168c. They are under the AGPL, version 3 or later
(LICENSE), as the rest of this service. The source is in the zslice repository. The API is in
docs/embedding/viewer.md of zslice.

To update them, build zslice at a newer commit, copy the files of zig-out/web/view here (all but
example.html), and change the commit above.
