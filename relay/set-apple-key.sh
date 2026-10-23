#!/bin/sh
# Writes the APNs key into the secret of the relay, and restarts the relay so that it reads it.
#
#   relay/set-apple-key.sh KEY_ID TEAM_ID AuthKey_KEYID.p8
#
# The key goes from the .p8 file to Secrets Manager through a temporary file that only you can
# read. It is not printed, and it is not in an argument of a command. relay_secret stays as it is,
# so the send tokens of the apps stay valid.
set -eu
KEY_ID="$1"
TEAM_ID="$2"
P8="$3"
[ -r "$P8" ] || { echo "cannot read $P8" >&2; exit 1; }

umask 077
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
aws secretsmanager get-secret-value --secret-id printdeck-relay --query SecretString --output text |
  KEY_ID="$KEY_ID" TEAM_ID="$TEAM_ID" P8_FILE="$P8" python3 -c '
import json, os, sys
s = json.loads(sys.stdin.read())
s["key_id"], s["team_id"] = os.environ["KEY_ID"], os.environ["TEAM_ID"]
with open(os.environ["P8_FILE"]) as f:
    s["p8"] = f.read()
sys.stdout.write(json.dumps(s))
' > "$TMP"
aws secretsmanager put-secret-value --secret-id printdeck-relay --secret-string "file://$TMP" --query VersionId --output text >/dev/null
# A change of the configuration starts new instances of the function, which read the new secret.
aws lambda update-function-configuration --function-name printdeck-relay \
  --description "APNs key $KEY_ID, set $(date -u +%Y-%m-%dT%H:%MZ)" --query LastModified --output text >/dev/null
echo "The relay has the APNs key $KEY_ID of the team $TEAM_ID."
