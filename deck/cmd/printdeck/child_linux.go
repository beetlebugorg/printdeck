package main

import "syscall"

// childAttr makes the kernel end the Klipper host when the app ends.
func childAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM} }
