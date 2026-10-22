//go:build !linux

package system

import (
	"errors"
	"io"
	"net"
)

// The system of the Printdeck OS runs only on the deck. The desktop app has none of it.

var errNoSystem = errors.New("the Printdeck OS runs only on the deck")

func MountConfig() (func(), error)          { return nil, errNoSystem }
func Commit(log io.Writer) error            { return errNoSystem }
func Settings(log io.Writer) error          { return errNoSystem }
func peerUID(*net.UnixConn) (uint32, error) { return 0, errNoSystem }

// Init is the first process of the Printdeck OS.
func Init() { panic(errNoSystem) }
