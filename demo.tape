# Prerequisites:
#   go build -o metroshell .
#   (mapdata/delhi-ncr.mbtiles must be present)
#
# Install vhs:  go install github.com/charmbracelet/vhs@latest
# Run:          vhs demo.tape
#
# Zoom keys used:
#   + / -   →  ±0.25  (coarse keyboard zoom)
#   , / .   →  ±0.10  (fine zoom, mirrors scroll wheel — see main.go)

Output demo.gif

Set Width        1280
Set Height       800
Set FontSize     20
Set Padding      10
Set TypingSpeed  30ms
Set WaitTimeout  30s
Set Theme        { "name": "AMOLED-Whimsy", "black": "#535178", "red": "#ef6487", "green": "#5eca89", "yellow": "#fdd877", "blue": "#65aef7", "magenta": "#aa7ff0", "cyan": "#43c1be", "white": "#ffffff", "brightBlack": "#535178", "brightRed": "#ef6487", "brightGreen": "#5eca89", "brightYellow": "#fdd877", "brightBlue": "#65aef7", "brightMagenta": "#aa7ff0", "brightCyan": "#43c1be", "brightWhite": "#ffffff", "background": "#000000", "foreground": "#b3b0d6", "selection": "#3d3c58", "cursor": "#b3b0d6" }
Set WindowBar    Colorful
Set BorderRadius 10

# ── Launch ────────────────────────────────────────────────────────────
Show
Type "./metroshell mapdata/delhi-ncr.mbtiles"
Enter
#Sleep 3s

# ── Opening overview — let the full NCR breathe ───────────────────────
Sleep 1.2s


# ── Fine zoom in  (, = +0.1 per press) — smooth, continuous feel ──────
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms

#only till 13
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
Type ","
Sleep 20ms
#till 14
Right 2
Sleep 100ms 
Right 3
Sleep 100ms 
Up 2
Sleep 100ms
Up 2
Sleep 100ms
Up 2
Sleep 100ms
Up 2
Sleep 100ms


# ── Coarse keyboard zoom in  (+ = +0.25 per press) — snappy steps ────
Type "+"
Sleep 20ms
Type "+"
Sleep 20ms
Type "+"
Sleep 20ms
Type "+"
Sleep 1.2s

# ── Pan south-west toward Dwarka / Gurugram border ───────────────────
Left 4
Sleep 100ms
Left 4
Sleep 100ms
Down 4
Sleep 100ms
Down 3
Sleep 500ms

# ── Fine zoom out  (. = -0.1 per press) ──────────────────────────────
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#till 14
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#till 13

# ── Coarse keyboard zoom out  (- = -0.25 per press) ──────────────────
Type "-"
Sleep 20ms
Type "-"
Sleep 20ms
Type "-"
Sleep 20ms
Type "-"
Sleep 20ms
#till 12
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#11
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#10
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#9
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#8
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#7
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#6
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
Type "."
Sleep 20ms
#5


# ── Help overlay ──────────────────────────────────────────────────────
Type "?"
Sleep 2s

# ── Close help → quit ─────────────────────────────────────────────────
Type "?"
Sleep 120ms
Type "q"
