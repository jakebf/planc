# VHS tape file for generating planc static screenshot.
# Install: go install github.com/charmbracelet/vhs@latest
# Run:     vhs screenshot.tape

Output demo.png

Set Shell fish
Set Width 2400
Set Height 1400
Set FontSize 28
Set Theme "Catppuccin Mocha"
Set TypingSpeed 80ms

Type "./planc --demo"
Enter
Sleep 10s

# Toggle to "all" view so done plans are visible in screenshot
Type "a"
Sleep 1s

# Navigate down a couple items then back so preview is rendered
Type "j"
Sleep 1s
Type "k"
Sleep 1s

Screenshot demo.png
Sleep 1s
