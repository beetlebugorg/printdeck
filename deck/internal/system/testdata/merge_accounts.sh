#!/bin/sh
# The account merge of the shell version of printdeck-init: the reference for TestMergeAccounts.
# U is the writable copy of /etc, L the /etc of the image.
U=$1 L=$2
  if [ -f "$U/group" ]; then
    awk -F: -v OFS=: '
      NR == FNR { up[$1] = 1; used[$3] = 1; next }
      { lo[++n] = $0 }
      END {
        while ((getline l < ARGV[1]) > 0) {
          split(l, f, ":")
          for (k = 1; k <= n; k++) {
            split(lo[k], g, ":")
            if (g[1] != f[1]) continue
            m = split(g[4], want, ",")
            for (i = 1; i <= m; i++)
              if (want[i] != "" && ("," f[4] ",") !~ ("," want[i] ","))
                f[4] = (f[4] == "" ? want[i] : f[4] "," want[i])
          }
          print f[1], f[2], f[3], f[4]
        }
        for (k = 1; k <= n; k++) {
          split(lo[k], g, ":")
          if (g[1] in up) continue
          id = g[3]
          if (id in used) { for (id = 100; id in used; id++); }
          used[id] = 1
          print g[1], g[2], id, g[4]
        }
      }
    ' "$U/group" "$L/group" > "$U/.merge" && cat "$U/.merge" > "$U/group"
  fi
  if [ -f "$U/passwd" ]; then
    awk -F: -v OFS=: '
      FILENAME == ARGV[1] { gid[$1] = $3; next }
      FILENAME == ARGV[2] { gname[$3] = $1; next }
      FILENAME == ARGV[3] { up[$1] = 1; used[$3] = 1; print; next }
      !($1 in up) {
        id = $3
        if (id in used) { for (id = 100; id in used; id++); }
        used[id] = 1
        g = ($4 in gname && gname[$4] in gid) ? gid[gname[$4]] : $4
        print $1, $2, id, g, $5, $6, $7
      }
    ' "$U/group" "$L/group" "$U/passwd" "$L/passwd" > "$U/.merge" && cat "$U/.merge" > "$U/passwd"
  fi
  if [ -f "$U/shadow" ]; then
    awk -F: 'NR == FNR { seen[$1] = 1; next } !($1 in seen)' "$U/shadow" "$L/shadow" > "$U/.merge"
    cat "$U/.merge" >> "$U/shadow"
  fi
  rm -f "$U/.merge"
