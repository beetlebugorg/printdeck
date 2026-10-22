// Package serialport opens a serial port (a UART or a USB serial device) in raw mode at any baud
// rate, with the control lines that the boards of Klipper need for their resets. On Linux, it uses
// the termios2 interface, and Go's runtime poller for the reads, so a read waits without a thread.
// On macOS and Windows, it uses go.bug.st/serial.
package serialport
