#!/bin/zsh
# Body for an Automator "Run Shell Script" step (Quick Action). See
# docs/docs/tutorials/integration-recipes.md.
#
# Automator and Shortcuts do not read ~/.zshrc and use a minimal PATH, so the
# mp path and the API key are set explicitly here. Edit these three lines.
MP=/usr/local/bin/mp                 # output of `command -v mp` in Terminal
export MP_API_KEY="paste-your-key"   # an editor key: Settings > API, or `mp api key create quick-action --role editor`
export MP_API_URL="http://localhost:44557"

# Finder items: set "Pass input" to "as arguments" -> files arrive in "$@".
# Selected text: set "Pass input" to "to stdin" -> text arrives on stdin.
if [ "$#" -gt 0 ]; then
  # Folders cannot be uploaded; send only the regular files.
  files=()
  for f in "$@"; do
    [ -f "$f" ] && files+=("$f")
  done
  [ "${#files[@]}" -gt 0 ] || exit 0
  "$MP" clip upload -q "${files[@]}"
else
  "$MP" clip upload --filename selection.txt -q
fi
