cask "mahpastes" do
  version "@VERSION@"
  sha256 "@SHA_MACOS@"

  url "https://github.com/egeozcan/mahpastes/releases/download/v#{version}/mahpastes-macos-universal.zip"
  name "mahpastes"
  desc "Clipboard and file clip manager with an mp command-line client"
  homepage "https://github.com/egeozcan/mahpastes"

  livecheck do
    url :url
    strategy :github_latest
  end

  # A bare symbol means "this release or newer"; the ">= :ventura" string form
  # is deprecated.
  depends_on macos: :ventura

  # The release zip wraps everything in one folder.
  app "mahpastes-macos-universal/mahpastes.app"
  binary "mahpastes-macos-universal/mp"

  # The bundle identifier set by scripts/macos/build-file-provider.sh.
  # Quit only. Homebrew runs every other uninstall directive on upgrade and
  # reinstall too, so deleting the Launch at login agent here would switch the
  # setting off with every update. It goes with zap; see the caveats.
  uninstall quit: "io.github.egeozcan.mahpastes"

  # The File Provider's group container and its scripts folder are prefixed
  # with the signing team identifier, hence the globs.
  zap trash: [
    "~/Library/Application Scripts/*.io.github.egeozcan.mahpastes",
    "~/Library/Application Scripts/io.github.egeozcan.mahpastes.FileProvider",
    "~/Library/Application Support/FileProvider/io.github.egeozcan.mahpastes.FileProvider",
    "~/Library/Application Support/mahpastes",
    "~/Library/Caches/io.github.egeozcan.mahpastes",
    "~/Library/Caches/mahpastes",
    "~/Library/Containers/io.github.egeozcan.mahpastes.FileProvider",
    "~/Library/Group Containers/*.io.github.egeozcan.mahpastes",
    "~/Library/HTTPStorages/io.github.egeozcan.mahpastes",
    "~/Library/LaunchAgents/io.github.egeozcan.mahpastes.login.plist",
    "~/Library/Preferences/io.github.egeozcan.mahpastes.plist",
    "~/Library/Saved Application State/io.github.egeozcan.mahpastes.savedState",
    "~/Library/WebKit/io.github.egeozcan.mahpastes",
  ]

  caveats <<~EOS
    If Launch at login is on, switch it off in mahpastes' settings before
    uninstalling. A plain uninstall keeps the login item so that upgrades do
    not reset the setting; `brew uninstall --zap --cask mahpastes` removes it
    together with the clips and settings.
  EOS
end
