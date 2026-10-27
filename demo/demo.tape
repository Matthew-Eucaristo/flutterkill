# Record with: vhs demo/demo.tape   (run from the repository root; macOS or Linux)
# Everything runs against demo/setup.sh's synthetic HOME; on macOS that HOME is a
# small disk image, so the Trash used here is the image's own and goes with it.
Output demo/demo.gif
Set Shell bash
Set FontSize 15
Set Width 1280
Set Height 640
Set Padding 16
Set Framerate 20
Set TypingSpeed 60ms

Hide
Type "source demo/setup.sh && clear" Enter
Sleep 25s
Show

# Projects, biggest first, with last activity and a safety label.
Type "flutterkill" Sleep 400ms Enter
Sleep 3s
# Oldest first: the archived project, untouched for two years, rises to the top.
Type "o" Sleep 2s
# The cursor follows it: open it and pick two artifacts.
Type "l" Sleep 1s
Down Sleep 600ms
Space Sleep 500ms
Down Sleep 600ms
Space Sleep 1s
# The confirmation lists exactly what moves to the Trash.
Type "d" Sleep 2.5s
Type "y" Sleep 2.5s
# Machine-wide caches: which SDK and Gradle versions a project still pins.
Tab Sleep 2s
Down Sleep 500ms
Down Sleep 1.8s
Down Sleep 1.5s
Down Sleep 1.8s
# The legend, then quit: the receipt keeps measured numbers.
Type "?" Sleep 3s
Type "q" Sleep 400ms
Type "q" Sleep 3.5s

Hide
Type "cd / && { command -v hdiutil >/dev/null && hdiutil detach -quiet /tmp/flutterkill-demo/home; rm -rf /tmp/flutterkill-demo; }" Enter
Sleep 4s
