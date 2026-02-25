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

# Let the viewer take in the UI
Sleep 2s

# Browse down to a plan
Type "j"
Sleep 1.5s
Type "j"
Sleep 2s

# Add a label
Type "l"
Sleep 2.5s
Type "j"
Sleep 1s
Enter
Sleep 3.5s

# Enter view mode (comment mode)
Enter
Sleep 3s

# Browse the table of contents
Type "j"
Sleep 1.5s
Type "j"
Sleep 1.5s

# Add a comment on this heading
Enter
Sleep 1.5s
Type "This is great, Clod! xoxox"
Sleep 2s
Enter
Sleep 3.5s

# Change status to done via modal
Type "s"
Sleep 2.5s
Type "j"
Sleep 0.5s
Type "j"
Sleep 0.5s
Type "j"
Sleep 1s
Enter
Sleep 3s

# Exit view mode back to list
Escape
Sleep 2.5s

# Browse to another plan
Type "j"
Sleep 1.5s
Type "j"
Sleep 1.5s
Type "k"
Sleep 2s

# Open in Claude Code
Type "c"
Sleep 10s

# Exit back to main screen
Type "/exit"
Enter
Sleep 3s
