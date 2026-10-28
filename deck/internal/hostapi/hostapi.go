// Package hostapi has the version of the interface between the deck program and the programs of
// the running system: the API socket of the Klipper host (printdeck klippy) and the requests of
// the system helper (printdeck helper).
//
// After an update during a print, the deck program of the new slot can run with the Klipper host
// and the helper of the old slot until the restart. The deck does this switch only when the
// running Klipper host reports the Version of the new deck program.
package hostapi

// Version is the version of the interface. Increase it when a change to the deck needs an
// endpoint, a status field or a helper action that an earlier host does not have, or when the
// meaning of one changes.
const Version = 1

// InfoKey is the field of the answer to "info" of the Klipper host that has the Version.
const InfoKey = "printdeck_api"
