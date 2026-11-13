# Bed level tests

Five flat squares, 0.4 mm high: one in each corner of an area, and one in the middle. They show
how flat the first layer is across the bed.

| File | Printer | Area | Squares |
|---|---|---|---|
| `taz.stl` | TAZ Workhorse (280 x 280 mm bed) | 250 x 250 mm | 40 x 40 mm |
| `mini2.stl` | Mini 2 (160 x 160 mm bed) | 150 x 150 mm | 30 x 30 mm |

## Use

1. Slice with the first layer at 0.25 mm. Cura puts the test in the middle of the bed.
2. Print it with the Z offset of the deck at 0.
3. When the bed is cool, measure each square with calipers. Note the smallest and the largest
   value of each square.

About 0.30 mm on every square is a good first layer: the calipers read the tops of the lines.
Squares that differ by more than about 0.05 mm show a fault in the bed mesh or the probe.

## Make the files again

```
uv run --no-project --with trimesh --with numpy python make.py
```
