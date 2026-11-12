cask "mahpastes" do
  version "@VERSION@"
  sha256 "@SHA_MACOS@"

  url "https://github.com/egeozcan/mahpastes/releases/download/v#{version}/mahpastes-macos-universal.zip"
  name "mahpastes"
  desc "Clipboard and file clip manager with an mp command line client"
  homepage "https://github.com/egeozcan/mahpastes"

  livecheck do
    url :url
    strategy :github_latest
  end

  depends_on macos: ">= :ventura"

  # The release zip wraps everything in one folder.
  app "mahpastes-macos-universal/mahpastes.app"
  binary "mahpastes-macos-universal/mp"

  # The bundle identifier set by scripts/macos/build-file-provider.sh.
  uninstall quit: "io.github.egeozcan.mahpastes"

  zap trash: [
    "~/Library/Application Support/mahpastes",
    "~/Library/Caches/io.github.egeozcan.mahpastes",
    "~/Library/HTTPStorages/io.github.egeozcan.mahpastes",
    "~/Library/Preferences/io.github.egeozcan.mahpastes.plist",
    "~/Library/Saved Application State/io.github.egeozcan.mahpastes.savedState",
    "~/Library/WebKit/io.github.egeozcan.mahpastes",
  ]
end
