# Package manager metadata

Templates for installing mahpastes with Scoop (Windows) and Homebrew (macOS).
Nothing here is published anywhere; the repository does not own a bucket or a
tap.

- `scoop/mahpastes.json`: Scoop manifest template. Installs the Windows zip,
  puts `mp.exe` on `PATH` and adds a Start Menu shortcut.
- `homebrew/mahpastes.rb`: Homebrew cask template. Installs `mahpastes.app` and
  links `mp`.
- `render.sh`: fills `@VERSION@` and the SHA-256 placeholders from a release's
  `SHA256SUMS`.

Both templates depend on the layout of the release zips (one top-level folder
holding the app and `mp`). Change `.github/workflows/build.yml` packaging and
these files together.

## Release assets

The release workflow runs `render.sh` after writing `SHA256SUMS` and uploads
`mahpastes.json` and `mahpastes.rb` with the other assets. Those match the
published zips exactly, so this works without any manual edit:

```powershell
scoop install https://github.com/egeozcan/mahpastes/releases/latest/download/mahpastes.json
```

A manifest installed from a URL is not tracked by `scoop update`.

## Updating a bucket or tap by hand

1. Download `SHA256SUMS` from the release.
2. `bash packaging/render.sh v1.2.3 SHA256SUMS out/`
3. Scoop: commit `out/mahpastes.json` as `bucket/mahpastes.json` in your
   bucket. Its `checkver` and `autoupdate` entries let Scoop's `checkver -u`
   (or the Excavator bot) bump later versions.
4. Homebrew: commit `out/mahpastes.rb` as `Casks/mahpastes.rb` in your tap, then
   `brew audit --cask --online mahpastes` and `brew install --cask ./mahpastes.rb`
   on a Mac. Homebrew quarantines casks, so the macOS release must be signed and
   notarized (it is when the signing secrets are set). `binary "mp"` needs the
   notarized `mp` in the zip.

Pushing to a tap from CI needs a token for that repository; `GITHUB_TOKEN`
cannot do it, so bumping stays a manual step or a workflow in the tap.
