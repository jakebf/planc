# VHS tape file for generating planc demo screenshot/GIF.
# Install: go install github.com/charmbracelet/vhs@latest
# Run:     vhs demo.tape

Output demo.gif

Set Shell fish
Set Width 2400
Set Height 1400
Set FontSize 28
Set Theme "Catppuccin Mocha"
Set TypingSpeed 80ms

# Launch (hidden — start recording after UI is ready)
Hide
Type "./planc --demo"
Enter
Sleep 5s
Show

# Browse plans
Sleep 1.5s
Type "j"
Sleep 1.5s
Type "j"
Sleep 1.5s
Type "j"
Sleep 1.5s

# Set status on a couple
Type "s"
Sleep 2s
Type "s"
Sleep 2s

# Back up and launch Clod Code
Type "k"
Sleep 1.5s
Enter
Sleep 10s

# Exit back to main screen
Type "/exit"
Enter
Sleep 3s
