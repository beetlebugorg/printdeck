#!/bin/sh
# Makes the configs of the simulated printers: each one is the config of a real printer on the
# simulated board of the deck (serial: sim), with the thermistors that the board simulates. The
# deck and "printdeck app -sim" use them. Run it after a change to printer/printer.cfg or to
# ../examples/printers.gz.
set -e
cd "$(dirname "$0")"

# sim changes a printer config for the simulated board.
sim() {
	sed -e '0,/^serial:/s/^serial:.*/serial: sim/' \
		-e '/^restart_method:/d' \
		-e 's/^sensor_type:.*/sensor_type: Generic 3950   # the thermistor of the simulated board/'
}

{
	echo '# Printdeck simulated printer: taz-workhorse'
	echo '# printer/printer.cfg (the LulzBot TAZ Workhorse) on the simulated board of the deck'
	echo '# (serial: sim), with the thermistors that it simulates. Make it again with make.sh.'
	sim < ../../../../printer/printer.cfg
} > taz-workhorse.cfg

{
	echo '# Printdeck simulated printer: mini2'
	echo "# Klipper's printer-lulzbot-mini2-2018.cfg (the LulzBot Mini 2) on the simulated board of the"
	echo '# deck (serial: sim), with the thermistors that it simulates. Make it again with make.sh.'
	echo
	gzip -dc ../examples/printers.gz |
		awk '/^\036/ { on = ($0 == "\036printer-lulzbot-mini2-2018.cfg"); next } on' |
		sim
} > mini2.cfg

{
	echo '# Printdeck simulated printer: cr10-v3'
	echo "# Klipper's printer-creality-cr10-v3-2020.cfg (the Creality CR-10 V3) on the simulated board"
	echo '# of the deck (serial: sim), with the thermistors that it simulates. Make it again with make.sh.'
	echo
	gzip -dc ../examples/printers.gz |
		awk '/^\036/ { on = ($0 == "\036printer-creality-cr10-v3-2020.cfg"); next } on' |
		sim
} > cr10-v3.cfg
