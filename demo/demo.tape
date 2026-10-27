# Record with: vhs demo/demo.tape   (run from the repository root; macOS or Linux)
# Everything runs against demo/setup.sh's synthetic HOME; on macOS that HOME is a
# small disk image, so the Trash used here is the image's own and goes with it.
Output demo/demo.gif
Set Shell bash
Set FontSize 15
Set Width 1280
Set Height 640
Set Padding 16
Set Framerate 24
Set TypingSpeed 60ms

Hide
Type "source demo/setup.sh && clear" Enter
Sleep 20s
Show

Type "flutterkill" Sleep 400ms Enter
Sleep 2.5s
Type "l" Sleep 800ms
Down Sleep 500ms
Space Sleep 400ms
Down Sleep 500ms
Space Sleep 800ms
Type "d" Sleep 2s
Type "y" Sleep 2s
Type "o" Sleep 1.2s
Tab Sleep 1.5s
Down Sleep 500ms
Down Sleep 2.5s
Type "?" Sleep 3s
Type "q" Sleep 400ms
Type "q" Sleep 3s

Hide
Type "cd / && { command -v hdiutil >/dev/null && hdiutil detach -quiet /tmp/flutterkill-demo/home; rm -rf /tmp/flutterkill-demo; }" Enter
Sleep 3s
