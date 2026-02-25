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

# Browse plans
Type "j"
Sleep 1.5s
Type "j"
Sleep 2s

# Set status via modal — mark as done
Type "s"
Sleep 2.5s
Type "j"
Sleep 1.5s
Enter
Sleep 3s

# Browse down through the list
Type "j"
Sleep 1.5s
Type "j"
Sleep 1.5s
Type "j"
Sleep 2s

# Show all plans (toggle to see everything including done)
Type "a"
Sleep 3s

# Browse around a bit
Type "j"
Sleep 1.5s
Type "j"
Sleep 1.5s
Type "k"
Sleep 2s

# Add an existing label to this plan
Type "l"
Sleep 2.5s
Type "j"
Sleep 1s
Enter
Sleep 3.5s

# Cycle label filter — pause longer so viewer notices list changing
Type "]"
Sleep 3s
Type "]"
Sleep 3s
Type "]"
Sleep 3s

# Switch to preview pane with arrow key
Right
Sleep 2s
Type "j"
Sleep 1s
Type "j"
Sleep 1s
Type "j"
Sleep 2s

# Back to list
Left
Sleep 2s

# Launch Claude Code
Type "k"
Sleep 1s
Enter
Sleep 10s

# Exit back to main screen
Type "/exit"
Enter
Sleep 3s
