package render

import (
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// signalQueue publishes body before metadata, retries in canonical order, and
// retains unacknowledged records. The target receives no install credential.
func signalQueue(gateway, task, installToken string, wget bool) string {
	scope := "finalize"
	credential := "LOBER_TOKEN=$(cat \"$DIR/finalize.token\") || return 1"
	if installToken != "" {
		scope = "install"
		credential = "LOBER_TOKEN=" + shQuote(installToken)
	}
	base := catalog.SignalURL(gateway, task, "TOKEN", "")
	before, after := tokenURLParts(base)
	transport := "curl -fsS -m 3 -X POST -H 'Content-Type: text/plain' \"$q_address\" --data-binary \"@$q_file.body\""
	if wget {
		transport = "/usr/bin/wget -q -T 3 --tries=1 -O /dev/null --header='Content-Type: text/plain' --post-file=\"$q_file.body\" \"$q_address\""
	}
	return "OUTBOX_NAME=outbox-" + scope + "\n" + `flush_signals() {
 ` + credential + `
 for q_meta in "$DIR/$OUTBOX_NAME/"*.meta; do
  [ -f "$q_meta" ] || continue
  q_path=$(cat "$q_meta") || continue
  q_file=${q_meta%.meta}
  q_address=` + shQuote(before) + `"$LOBER_TOKEN"` + shQuote(after) + `"$q_path"
  if ` + transport + ` >/dev/null 2>&1; then
   rm -f "$q_meta" "$q_file.body"
  else
   return 1
  fi
 done
 return 0
}
queue_signal() {
 q_label=$1; q_request=$2; q_body=$3
 mkdir -p "$DIR/$OUTBOX_NAME" || return 1
 q_entry="$DIR/$OUTBOX_NAME/$q_label"
 if [ ! -f "$q_entry.meta" ]; then
  if [ -n "$q_body" ]; then cp "$q_body" "$q_entry.body.tmp.$$" || return 1; else : > "$q_entry.body.tmp.$$" || return 1; fi
  mv "$q_entry.body.tmp.$$" "$q_entry.body" || return 1
  printf '%s\n' "$q_request" > "$q_entry.meta.tmp.$$" || return 1
  mv "$q_entry.meta.tmp.$$" "$q_entry.meta" || return 1
 fi
 flush_signals || :
}
`
}

func queuedNativeSignal(phase model.SignalPhase) string {
	label := map[model.SignalPhase]string{model.PhaseBooted: "000-booted", model.PhaseInitStart: "001-init-start", model.PhaseInitDone: "900-init-done", model.PhaseReady: "901-ready"}[phase]
	return "queue_signal " + shQuote(label) + " " + shQuote(string(phase)) + " ''\n"
}
