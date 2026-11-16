# Checks before and after the first print

The CAD gives the head, the carriage and the X ends. These values are not in the CAD, or the CAD and a
measurement do not agree. Check them on the printer.

| # | Value in `shape.py` | Now | How to check |
|---|---|---|---|
| 1 | `LIP_PLAY`, the blower outlet | 0.3 mm a side; outlet 12.2 x 23.1 mm inside (ESTIMATE: 1 mm walls) | Print the lip test. Push it into the blower outlet. It must go in by hand and stay in. |
| 2 | `XEND_FRONT_Y`, the front of the right X end | 6.6 mm in front of the nozzle line. The CAD gives 1.6 mm. | At X max, measure from the front face of the motor back to the front of the X end. Expect 25 mm or more (CAD 30 mm). The blower stops 2 mm in front of it. |
| 3 | `CONNECTOR`, `PLUG_ROOM`, the motor plug | CAD: the connector under the motor at the end cap; 4 mm for the plug and its cable | Look under the motor. If the plug or its cable hangs more than 4 mm below the motor near the end cap, the link of the duct touches it. |
| 4 | `SPACER_T`, the front tab of the stock duct | 5.97 mm between the Aero and the motor (CAD) | When you remove the stock duct, measure its front tab. The spacer must have the same thickness. |
| 5 | `X_HOME_SHIFT`, X home | The CAD puts the X motor 5.8 mm left of the BLTouch bracket | Home X once with a hand on the power switch. Look at the gap between the bracket and the left X end. |
| 6 | `BLT_GAP`, the BLTouch height | 3.0 mm over the nozzle tip | With the pin up, measure from the nozzle tip to the bottom of the BLTouch body: 2.3 to 4.3 mm. |
| 7 | The wiper pad | The BLTouch is now 25 mm left of the nozzle | Watch the first nozzle wipe. The BLTouch must pass over the pad. |
| 8 | `PIN_Y`, the BLTouch behind the fan | 1.4 mm to the fan edge, 0.8 mm to the X carriage (CAD) | Move the head by hand along X. The BLTouch cover must not touch the lower bearing holder of the carriage. |
