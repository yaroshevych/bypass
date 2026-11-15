# Homebrew release updates

The release workflow updates `yaroshevych/homebrew-tap` after publishing a stable `vMAJOR.MINOR.PATCH` release. Prereleases are skipped. The update job downloads the published assets, verifies their checksums, and pushes the formula. A failed update does not undo the release; inspect the job and update the formula manually if needed.

Before enabling this job, create a fine-grained GitHub personal access token with access only to `yaroshevych/homebrew-tap` and repository **Contents: Read and write** permission. Add it to the Bypass repository under Settings → Secrets and variables → Actions as `HOMEBREW_TAP_TOKEN`. The token owner must have write access to the tap. Rotate the secret before the token expires.

Run `python3 scripts/test-update-homebrew.py` to test the updater. To run it manually, download the release assets and `SHA256SUMS` into a directory, then run `python3 scripts/update-homebrew.py v0.3.1 RELEASE_DIR PATH_TO_TAP/Formula/bypass.rb`. Review and commit the resulting change in the tap.
