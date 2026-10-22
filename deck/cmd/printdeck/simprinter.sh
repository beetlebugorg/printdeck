#!/bin/sh
# Makes simprinter.cfg, the printer of "printdeck app -sim", from printer/printer.cfg: the serial
# port is the simulated board, and the thermistors are those that it simulates.
set -e
cd "$(dirname "$0")"
{
	echo '# The simulated printer of "printdeck app -sim": printer/printer.cfg (the LulzBot TAZ Workhorse)'
	echo '# on the simulated board of the deck (serial: sim), with the thermistors that it simulates.'
	echo '# Make it again from printer/printer.cfg with deck/cmd/printdeck/simprinter.sh.'
	sed -e '0,/^serial:/s/^serial:.*/serial: sim/' \
		-e '/^restart_method:/d' \
		-e 's/^sensor_type:.*/sensor_type: Generic 3950   # the thermistor of the simulated board/' \
		../../../printer/printer.cfg
} > simprinter.cfg
